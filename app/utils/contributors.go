package utils

import (
	"path/filepath"
	"sort"
	"strings"
)

func coverageAvailable(snapshot *Snapshot) bool {
	return snapshot.UnreadableEntries == len(snapshot.Issues)
}

func comparisonCoverage(state *SnapshotState) func(string) bool {
	affected := map[string]bool{}
	blocked := map[string]bool{}
	unknown := false
	for _, snapshot := range []*Snapshot{state.Baseline, state.Latest} {
		if !coverageAvailable(snapshot) {
			unknown = true
		}
		for _, issue := range snapshot.Issues {
			path, err := filepath.Rel(snapshot.Root, issue.Path)
			if err != nil {
				unknown = true
				continue
			}
			blocked[path] = true
			for {
				affected[path] = true
				if path == "." {
					break
				}
				parent := filepath.Dir(path)
				if parent == path {
					break
				}
				path = parent
			}
		}
	}
	return func(path string) bool {
		if unknown || affected[path] {
			return true
		}
		for {
			if blocked[path] {
				return true
			}
			if path == "." {
				return false
			}
			parent := filepath.Dir(path)
			if parent == path {
				return false
			}
			path = parent
		}
	}
}

func contributionRows(state *SnapshotState) []DirectoryChange {
	depth := min(state.Baseline.Depth, state.Latest.Depth)
	totals := map[string]int64{}
	for i, snapshot := range []*Snapshot{state.Baseline, state.Latest} {
		for path, entry := range snapshot.Directories {
			if directoryLevel(path) > depth {
				continue
			}
			if i == 0 {
				totals[path] -= entry.Size
			} else {
				totals[path] += entry.Size
			}
		}
	}
	residual := make(map[string]int64, len(totals))
	for path, delta := range totals {
		residual[path] = delta
	}
	for path, delta := range totals {
		if path != "." {
			residual[filepath.Dir(path)] -= delta
		}
	}
	uncertain := comparisonCoverage(state)
	rows := []DirectoryChange{}
	for path, delta := range residual {
		scope := "direct_files"
		if directoryLevel(path) == depth {
			scope = "subtree"
		}
		if delta == 0 {
			continue
		}
		kind := "modified"
		if _, exists := state.Baseline.Directories[path]; !exists {
			kind = "added"
		}
		if _, exists := state.Latest.Directories[path]; !exists {
			kind = "removed"
		}
		isUncertain := uncertain(path)
		if isUncertain {
			kind = "uncertain"
		}
		rows = append(rows, DirectoryChange{Delta: SignedSize(delta), DeltaBytes: delta, Kind: kind, Path: filepath.Join(state.Latest.Root, path), Scope: scope, Uncertain: isUncertain})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Uncertain != b.Uncertain {
			return !a.Uncertain
		}
		if (a.DeltaBytes > 0) != (b.DeltaBytes > 0) {
			return a.DeltaBytes > 0
		}
		x, y := a.DeltaBytes, b.DeltaBytes
		if x < 0 {
			x = -x
		}
		if y < 0 {
			y = -y
		}
		if x == y {
			return a.Path < b.Path
		}
		return x > y
	})
	return rows
}

func directoryLevel(path string) int {
	if path == "." {
		return 0
	}
	return strings.Count(path, string(filepath.Separator)) + 1
}

func SignedSize(bytes int64) string {
	if bytes < 0 {
		return "-" + FormatSize(-bytes)
	}
	return "+" + FormatSize(bytes)
}
