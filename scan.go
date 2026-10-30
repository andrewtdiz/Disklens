package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Node struct {
	Name     string
	Path     string
	Size     int64
	Modified time.Time
	Dir      bool
	Children []*Node
	Parent   *Node
	Delta    int64
	Compared bool
	Removed  bool
}

type Scan struct {
	Backend          string
	FallbackReason   string
	Fraction         float64
	Bytes            int64
	Root             *Node
	Files            int
	Folders          int
	Skipped          int
	Errors           int
	FirstError       string
	At               time.Time
	DirectoryBatches int
	FileQueries      int
}

type Entry struct {
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type Snapshot struct {
	Root        string           `json:"root"`
	Depth       int              `json:"depth"`
	At          time.Time        `json:"at"`
	Directories map[string]Entry `json:"directories"`
}

type Settings struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

func (s *Scan) recordError(path string, err error) {
	s.Errors++
	if s.FirstError == "" {
		s.FirstError = path + ": " + err.Error()
	}
}

func sortNodes(nodes []*Node) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Size == nodes[j].Size {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].Size > nodes[j].Size
	})
}

func takeSnapshot(s *Scan, depth int) *Snapshot {
	out := &Snapshot{Root: s.Root.Path, Depth: depth, At: s.At, Directories: map[string]Entry{}}
	var visit func(*Node, int)
	visit = func(n *Node, level int) {
		if !n.Dir || n.Removed || level > depth {
			return
		}
		rel, _ := filepath.Rel(s.Root.Path, n.Path)
		out.Directories[rel] = Entry{n.Size, n.Modified}
		for _, child := range n.Children {
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
		for _, child := range n.Children {
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
		rel, _ := filepath.Rel(root.Path, n.Path)
		current[rel] = n
		for _, c := range n.Children {
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
		n := &Node{Name: filepath.Base(rel), Path: filepath.Join(root.Path, rel), Dir: true, Parent: parent, Modified: entry.Modified, Delta: -entry.Size, Compared: true, Removed: true}
		parent.Children = append(parent.Children, n)
		current[rel] = n
	}
}

func dataDirectory() string {
	if dir := os.Getenv("DISKLENS_DATA"); dir != "" {
		return dir
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Disklens")
	}
	return filepath.Join(dir, "Disklens")
}

func snapshotPath(root string) string {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return filepath.Join(dataDirectory(), "snapshots", fmt.Sprintf("%x.json", sha256.Sum256([]byte(root))))
}

func saveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".disklens-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(b); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v, unit := float64(n), 0
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	for v >= 1024 && unit < len(units)-1 {
		v /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", v, units[unit])
}

func formatDelta(n *Node) string {
	if !n.Compared {
		return "—"
	}
	if n.Delta > 0 {
		return "+" + formatSize(n.Delta)
	}
	if n.Delta < 0 {
		return "−" + formatSize(-n.Delta)
	}
	return "0 B"
}
