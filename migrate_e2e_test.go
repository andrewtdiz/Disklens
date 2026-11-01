//go:build e2e && !cli

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateMigrationCLIParity(t *testing.T) {
	legacy, shared := t.TempDir(), t.TempDir()
	root := t.TempDir()
	baseline := &Snapshot{Root: root, Depth: 5, At: time.Now().Add(-time.Hour), Directories: map[string]Entry{".": {Size: 1024}, "Growth": {Size: 1024}}}
	latest := &Snapshot{Root: root, Depth: 5, At: time.Now(), Directories: map[string]Entry{".": {Size: 4096}, "Growth": {Size: 4096}}}
	t.Setenv("DISKLENS_DATA", legacy)
	if err := saveJSON(snapshotPath(root), &SnapshotState{Version: 1, Baseline: baseline, Latest: latest}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(snapshotPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyState(legacy, shared); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISKLENS_DATA", shared)
	state, err := loadSnapshotState(root)
	if err != nil {
		t.Fatal(err)
	}
	tree := &Node{Name: root, Dir: true, Size: 4096}
	child := &Node{Name: "Growth", Dir: true, Size: 4096, Parent: tree}
	tree.setChildren([]*Node{child})
	compareSaved(tree, state)
	var out, errors bytes.Buffer
	if code := runCLI([]string{"diff", root, "--json"}, &out, &errors); code != 0 {
		t.Fatalf("CLI: %s %s", &out, &errors)
	}
	var report DiffReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 2 || !report.BaselineAt.Equal(baseline.At) || !report.ScannedAt.Equal(latest.At) {
		t.Fatalf("migration changed comparison: %+v", report)
	}
	for _, change := range report.Changes {
		if change.DeltaBytes != tree.Delta || change.DeltaBytes != child.Delta {
			t.Fatal("desktop and CLI differ")
		}
	}
	state.Baseline = state.Latest
	if err := saveJSON(snapshotPath(root), state); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyState(legacy, shared); err != nil {
		t.Fatal(err)
	}
	current, err := loadSnapshotState(root)
	if err != nil || len(snapshotDiff(current).Changes) != 0 {
		t.Fatal("migration overwrote newer shared state", err)
	}
	old, err := os.ReadFile(filepath.Join(legacy, "snapshots", filepath.Base(snapshotPath(root))))
	if err != nil || !bytes.Equal(old, original) {
		t.Fatal("migration modified original backup", err)
	}
}
