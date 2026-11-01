package utils

import (
	"path/filepath"
	"sort"
	"time"
)

type Node struct {
	Name                   string
	Size                   int64
	Modified               fileTimestamp
	folder                 *directoryEntries
	Parent                 *Node
	Delta                  int64
	Links                  *fileLinks
	Dir, Compared, Removed bool
}

type directoryEntries struct {
	children []*Node
}

func (n *Node) Children() []*Node {
	if n.folder == nil {
		return nil
	}
	return n.folder.children
}

func (n *Node) addChild(child *Node) {
	if n.folder == nil {
		n.folder = &directoryEntries{}
	}
	n.folder.children = append(n.folder.children, child)
}

func (n *Node) setChildren(children []*Node) {
	if len(children) == 0 {
		n.folder = nil
		return
	}
	if n.folder == nil {
		n.folder = &directoryEntries{}
	}
	n.folder.children = children
}

type fileTimestamp uint64

func timestampFromTime(value time.Time) fileTimestamp {
	if value.IsZero() {
		return 0
	}
	return fileTimestamp(uint64(value.Unix()+11644473600)*10000000 + uint64(value.Nanosecond()/100) + 1)
}

func timestampFromNTFS(ticks uint64) fileTimestamp {
	return fileTimestamp(ticks + 1)
}

func (stamp fileTimestamp) Time() time.Time {
	if stamp == 0 {
		return time.Time{}
	}
	ticks := uint64(stamp) - 1
	return time.Unix(int64(ticks/10000000)-11644473600, int64(ticks%10000000)*100).Local()
}

func (n *Node) FullPath() string {
	if n.Parent == nil {
		return n.Name
	}
	parts := make([]string, 0, 16)
	for p := n; p != nil; p = p.Parent {
		parts = append(parts, p.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return filepath.Join(parts...)
}

type fileLinks struct {
	Nodes []*Node
}

func retainLinks(previous, next *Node) {
	if previous.Links == nil {
		previous.Links = &fileLinks{Nodes: []*Node{previous}}
	}
	next.Links = previous.Links
	next.Links.Nodes = append(next.Links.Nodes, next)
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
	UnreadableEntries int              `json:"unreadable_entries,omitempty"`
	Root              string           `json:"root"`
	Depth             int              `json:"depth"`
	At                time.Time        `json:"at"`
	Directories       map[string]Entry `json:"directories"`
}

type Settings struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

func (s *Scan) RecordError(path string, err error) {
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
