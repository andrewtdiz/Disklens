package utils

import (
	"path/filepath"
	"sort"
)

func TakeSnapshot(s *Scan, depth int) *Snapshot {
	out := &Snapshot{Root: s.Root.FullPath(), Depth: depth, At: s.At, Directories: map[string]Entry{}}
	var visit func(*Node, int)
	visit = func(n *Node, level int) {
		if !n.Dir || n.Removed || level > depth {
			return
		}
		rel, _ := filepath.Rel(s.Root.FullPath(), n.FullPath())
		out.Directories[rel] = Entry{n.Size, n.Modified.Time()}
		for _, child := range n.Children() {
			visit(child, level+1)
		}
	}
	visit(s.Root, 0)
	return out
}

func compare(root *Node, old *Snapshot) {
	var visit func(*Node, string, int)
	visit = func(n *Node, rel string, level int) {
		if !n.Dir || level > old.Depth {
			return
		}
		previous := old.Directories[rel]
		n.Compared = true
		n.Delta = n.Size - previous.Size
		for _, child := range n.Children() {
			visit(child, filepath.Join(rel, child.Name), level+1)
		}
	}
	visit(root, ".", 0)
	current := map[string]*Node{}
	var index func(*Node, int)
	index = func(n *Node, depth int) {
		if !n.Dir || depth > old.Depth {
			return
		}
		rel, _ := filepath.Rel(root.FullPath(), n.FullPath())
		current[rel] = n
		for _, c := range n.Children() {
			index(c, depth+1)
		}
	}
	index(root, 0)
	keys := make([]string, 0, len(old.Directories))
	for rel := range old.Directories {
		keys = append(keys, rel)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) < len(keys[j]) })
	for _, rel := range keys {
		if _, exists := current[rel]; exists {
			continue
		}
		parent := current[filepath.Dir(rel)]
		if parent == nil || !parent.Dir {
			continue
		}
		entry := old.Directories[rel]
		n := &Node{Name: filepath.Base(rel), Dir: true, Parent: parent, Modified: timestampFromTime(entry.Modified), Delta: -entry.Size, Compared: true, Removed: true}
		parent.addChild(n)
		current[rel] = n
	}
}
