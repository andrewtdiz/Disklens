package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SnapshotState struct {
	Version  int       `json:"version"`
	Baseline *Snapshot `json:"baseline,omitempty"`
	Latest   *Snapshot `json:"latest"`
}

type DirectoryChange struct {
	Delta      string `json:"delta"`
	DeltaBytes int64  `json:"delta_bytes"`
	Kind       string `json:"kind"`
	Path       string `json:"path"`
}

type DiffSummary struct {
	ScanInProgress      bool      `json:"scan_in_progress"`
	Status              string    `json:"status"`
	Root                string    `json:"root"`
	BaselineAt          time.Time `json:"baseline_at"`
	ScannedAt           time.Time `json:"scanned_at"`
	Depth               int       `json:"depth"`
	SizeBasis           string    `json:"size_basis"`
	IncludesDescendants bool      `json:"includes_descendants"`
}

type DiffReport struct {
	DiffSummary
	Changes []DirectoryChange `json:"changes"`
}

func loadSnapshotState(root string) (*SnapshotState, error) {
	b, err := os.ReadFile(snapshotPath(root))
	if os.IsNotExist(err) {
		return &SnapshotState{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var state SnapshotState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	if state.Version == 0 {
		var previous Snapshot
		if err := json.Unmarshal(b, &previous); err != nil {
			return nil, err
		}
		if previous.Root == "" || previous.Directories == nil {
			return nil, fmt.Errorf("invalid saved scan")
		}
		return &SnapshotState{Version: 1, Latest: &previous}, nil
	}
	if state.Version != 1 || state.Latest == nil || state.Latest.Directories == nil || state.Latest.Root == "" {
		return nil, fmt.Errorf("unsupported or invalid saved scan")
	}
	if state.Baseline != nil && (state.Baseline.Directories == nil || snapshotPath(state.Baseline.Root) != snapshotPath(state.Latest.Root)) {
		return nil, fmt.Errorf("invalid saved baseline")
	}
	return &state, nil
}

func snapshotDiff(state *SnapshotState) DiffReport {
	old, latest := state.Baseline, state.Latest
	depth := min(old.Depth, latest.Depth)
	report := DiffReport{DiffSummary: DiffSummary{Status: "ok", Root: latest.Root, BaselineAt: old.At.UTC(), ScannedAt: latest.At.UTC(), Depth: depth, SizeBasis: "allocated_bytes", IncludesDescendants: true}, Changes: []DirectoryChange{}}
	keys := make(map[string]struct{}, len(old.Directories)+len(latest.Directories))
	for path := range old.Directories {
		keys[path] = struct{}{}
	}
	for path := range latest.Directories {
		keys[path] = struct{}{}
	}
	for path := range keys {
		level := 0
		if path != "." {
			level = strings.Count(path, string(filepath.Separator)) + 1
		}
		if level > depth {
			continue
		}
		before, existed := old.Directories[path]
		after, exists := latest.Directories[path]
		delta := after.Size - before.Size
		if delta == 0 {
			continue
		}
		kind := "modified"
		if !existed {
			kind = "added"
		} else if !exists {
			kind = "removed"
		}
		display := "+" + formatSize(delta)
		if delta < 0 {
			display = "-" + formatSize(-delta)
		}
		report.Changes = append(report.Changes, DirectoryChange{Delta: display, DeltaBytes: delta, Kind: kind, Path: filepath.Join(latest.Root, path)})
	}
	magnitude := func(value int64) int64 {
		if value < 0 {
			return -value
		}
		return value
	}
	sort.Slice(report.Changes, func(i, j int) bool {
		a, b := report.Changes[i], report.Changes[j]
		if magnitude(a.DeltaBytes) == magnitude(b.DeltaBytes) {
			return a.Path < b.Path
		}
		return magnitude(a.DeltaBytes) > magnitude(b.DeltaBytes)
	})
	return report
}

func clearComparison(root *Node) {
	root.Compared, root.Delta = false, 0
	children := root.Children()[:0]
	for _, child := range root.Children() {
		if child.Removed {
			continue
		}
		if child.Dir && child.Compared {
			clearComparison(child)
		}
		children = append(children, child)
	}
	clear(root.Children()[len(children):])
	root.setChildren(children)
}

func compareSaved(root *Node, state *SnapshotState) {
	if state.Baseline == nil {
		return
	}
	baseline := *state.Baseline
	baseline.Depth = min(baseline.Depth, state.Latest.Depth)
	baseline.Directories = make(map[string]Entry)
	for path, entry := range state.Baseline.Directories {
		level := 0
		if path != "." {
			level = strings.Count(path, string(filepath.Separator)) + 1
		}
		if level <= baseline.Depth {
			baseline.Directories[path] = entry
		}
	}
	compare(root, &baseline)
}
