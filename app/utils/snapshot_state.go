package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Scope      string `json:"scope"`
	Uncertain  bool   `json:"uncertain"`
}

type DiffSummary struct {
	BaselineOnly              bool      `json:"baseline_only"`
	NetBytes                  int64     `json:"net_bytes"`
	IncreasedBytes            int64     `json:"increased_bytes"`
	DecreasedBytes            int64     `json:"decreased_bytes"`
	UncertainBytes            int64     `json:"uncertain_bytes"`
	CoverageDetailsAvailable  bool      `json:"coverage_details_available"`
	Partial                   bool      `json:"partial"`
	BaselineUnreadableEntries int       `json:"baseline_unreadable_entries"`
	LatestUnreadableEntries   int       `json:"latest_unreadable_entries"`
	ScanInProgress            bool      `json:"scan_in_progress"`
	Status                    string    `json:"status"`
	Root                      string    `json:"root"`
	BaselineAt                time.Time `json:"baseline_at"`
	ScannedAt                 time.Time `json:"scanned_at"`
	Depth                     int       `json:"depth"`
	SizeBasis                 string    `json:"size_basis"`
	IncludesDescendants       bool      `json:"includes_descendants"`
}

type DiffReport struct {
	DiffSummary
	Changes []DirectoryChange `json:"changes"`
}

func LoadSnapshotState(root string) (*SnapshotState, error) {
	b, err := os.ReadFile(SnapshotPath(root))
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
	if state.Baseline != nil && (state.Baseline.Directories == nil || SnapshotPath(state.Baseline.Root) != SnapshotPath(state.Latest.Root)) {
		return nil, fmt.Errorf("invalid saved baseline")
	}
	return &state, nil
}

func SnapshotDiff(state *SnapshotState) DiffReport {
	old, latest := state.Baseline, state.Latest
	depth := min(old.Depth, latest.Depth)
	report := DiffReport{DiffSummary: DiffSummary{
		Partial:                   old.UnreadableEntries > 0 || latest.UnreadableEntries > 0,
		BaselineUnreadableEntries: old.UnreadableEntries, LatestUnreadableEntries: latest.UnreadableEntries,
		Status: "ok", Root: latest.Root, BaselineAt: old.At.UTC(), ScannedAt: latest.At.UTC(),
		Depth: depth, SizeBasis: "allocated_bytes", IncludesDescendants: false,
		BaselineOnly: old.At.Equal(latest.At), CoverageDetailsAvailable: coverageAvailable(old) && coverageAvailable(latest),
		NetBytes: latest.Directories["."].Size - old.Directories["."].Size,
	}, Changes: []DirectoryChange{}}
	report.Changes = ContributionRows(state, "", false)
	for _, change := range report.Changes {
		if change.Uncertain {
			report.UncertainBytes += change.DeltaBytes
		} else if change.DeltaBytes > 0 {
			report.IncreasedBytes += change.DeltaBytes
		} else {
			report.DecreasedBytes -= change.DeltaBytes
		}
	}
	return report
}

func ClearComparison(root *Node) {
	root.Compared, root.Delta, root.Uncertain = false, 0, false
	children := root.Children()[:0]
	for _, child := range root.Children() {
		if child.Removed {
			continue
		}
		if child.Dir && child.Compared {
			ClearComparison(child)
		}
		children = append(children, child)
	}
	clear(root.Children()[len(children):])
	root.setChildren(children)
}

func CompareSaved(root *Node, state *SnapshotState) {
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
	if state.Baseline.UnreadableEntries == 0 && state.Latest.UnreadableEntries == 0 {
		return
	}
	uncertain := comparisonCoverage(state)
	var mark func(*Node, string, int)
	mark = func(n *Node, relative string, level int) {
		if level > baseline.Depth {
			return
		}
		n.Uncertain = uncertain(relative)
		for _, child := range n.Children() {
			if child.Dir {
				mark(child, filepath.Join(relative, child.Name), level+1)
			}
		}
	}
	mark(root, ".", 0)
}
