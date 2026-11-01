//go:build !cli

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
)

type deletionResult struct {
	target            *Node
	replacement       *Scan
	err, reconcileErr error
	links             []linkUpdate
	lock              *os.File
}

type linkUpdate struct {
	node    *Node
	size    int64
	group   *fileLinks
	missing bool
}

func collectNodes(n *Node, visit func(*Node)) {
	if n == nil || n.Removed {
		return
	}
	visit(n)
	for _, child := range n.Children() {
		collectNodes(child, visit)
	}
}

func reconcileDeletedFolder(target *Node) deletionResult {
	r := deletionResult{target: target}
	info, err := os.Lstat(target.FullPath())
	if os.IsNotExist(err) {
		return reconcileDeletedLinks(r)
	}
	if err != nil {
		r.reconcileErr = err
		return r
	}
	if !info.IsDir() || skipEntry(info) {
		r.reconcileErr = fmt.Errorf("The selected folder was replaced; refresh to inspect the replacement")
		return r
	}
	r.replacement, r.reconcileErr = walkAffectedDirectory(context.Background(), target.FullPath())
	if r.reconcileErr == nil && r.replacement.Errors != 0 {
		r.reconcileErr = fmt.Errorf("Affected folder is partly unreadable: %s", r.replacement.FirstError)
	}
	return reconcileDeletedLinks(r)
}

func reconcileDeletedLinks(r deletionResult) deletionResult {
	groups := make(map[*fileLinks]bool)
	inside := make(map[*Node]bool)
	collectNodes(r.target, func(n *Node) {
		if n.Links != nil {
			inside[n] = true
			groups[n.Links] = true
		}
	})
	if len(groups) == 0 {
		return r
	}
	wanted := make(map[string]bool)
	for group := range groups {
		for _, n := range group.Nodes {
			wanted[n.FullPath()] = true
		}
	}
	fresh := make(map[string]*Node)
	if r.replacement != nil {
		collectNodes(r.replacement.Root, func(n *Node) {
			if wanted[n.FullPath()] {
				fresh[n.FullPath()] = n
			}
		})
	}
	candidates := make(map[*Node]bool)
	for group := range groups {
		for _, old := range group.Nodes {
			n := old
			if inside[old] {
				n = fresh[old.FullPath()]
			}
			if n == nil {
				continue
			}
			candidates[n] = true
			if inside[old] && n.Links != nil {
				for _, alias := range n.Links.Nodes {
					candidates[alias] = true
				}
			}
		}
	}
	current := make(map[string]*fileLinks)
	sizes := make(map[string]int64)
	for n := range candidates {
		info, err := os.Lstat(n.FullPath())
		if os.IsNotExist(err) {
			r.links = append(r.links, linkUpdate{node: n, missing: true})
			continue
		}
		if err != nil {
			r.reconcileErr = err
			continue
		}
		if info.IsDir() || skipEntry(info) {
			r.reconcileErr = fmt.Errorf("Hard-link peer changed: %s", n.FullPath())
			continue
		}
		size, key, err := diskSize(n.FullPath(), info)
		if err != nil {
			r.reconcileErr = err
			continue
		}
		if key == "" {
			key = n.FullPath()
		}
		if current[key] == nil {
			current[key] = &fileLinks{}
		}
		current[key].Nodes = append(current[key].Nodes, n)
		sizes[key] = size
	}
	for key, links := range current {
		sort.Slice(links.Nodes, func(i, j int) bool { return links.Nodes[i].FullPath() < links.Nodes[j].FullPath() })
		for i, n := range links.Nodes {
			size := int64(0)
			if i == 0 {
				size = sizes[key]
			}
			r.links = append(r.links, linkUpdate{node: n, size: size, group: links})
		}
	}
	return r
}

func changeNodeSize(n *Node, size int64) {
	delta := size - n.Size
	n.Size = size
	for p := n.Parent; p != nil; p = p.Parent {
		p.Size += delta
	}
}

func removeChild(n *Node) {
	p := n.Parent
	changeNodeSize(n, 0)
	for i, child := range p.Children() {
		if child == n {
			copy(p.Children()[i:], p.Children()[i+1:])
			p.Children()[len(p.Children())-1] = nil
			p.setChildren(p.Children()[:len(p.Children())-1])
			break
		}
	}
}

func (a *app) applyDeletion(r deletionResult) {
	clearComparison(a.scan.Root)
	oldFiles, oldFolders := 0, 0
	collectNodes(r.target, func(n *Node) {
		if n.Dir {
			oldFolders++
			a.outline.Open.Remove(n)
		} else {
			oldFiles++
		}
	})
	parent := r.target.Parent
	dirty := make(map[*Node]bool)
	mark := func(n *Node) {
		for p := n.Parent; p != nil; p = p.Parent {
			dirty[p] = true
		}
	}
	mark(r.target)
	removeChild(r.target)
	a.scan.Files -= oldFiles
	a.scan.Folders -= oldFolders
	if r.replacement != nil {
		n := r.replacement.Root
		n.Name = r.target.Name
		size := n.Size
		n.Size, n.Parent = 0, parent
		parent.addChild(n)
		changeNodeSize(n, size)
		a.scan.Files += r.replacement.Files
		a.scan.Folders += r.replacement.Folders
	}
	for _, update := range r.links {
		mark(update.node)
		if update.missing {
			removeChild(update.node)
			a.scan.Files--
			continue
		}
		update.node.Links = update.group
		changeNodeSize(update.node, update.size)
	}
	for n := range dirty {
		sortNodes(n.Children())
	}
	a.scan.Bytes = a.scan.Root.Size
	a.focus, a.selected = parent, parent
	a.hover, a.mapRoot, a.mapColorRoot = nil, nil, nil
	a.selectionTrail = nil
	a.outline.Open.Add(parent)
	a.selectNode(parent)
	if r.reconcileErr != nil {
		a.scan.recordError(r.target.FullPath(), r.reconcileErr)
	}
}
