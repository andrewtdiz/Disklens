//go:build e2e && !cli

package utils

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStateMigrationCLIParity(t *testing.T) {
	cli := os.Getenv("DISKLENS_CLI_EXE")
	if cli == "" {
		t.Skip("Requires console build")
	}
	legacy, shared := t.TempDir(), t.TempDir()
	root := t.TempDir()
	baseline := &Snapshot{Root: root, Depth: 5, At: time.Now().Add(-time.Hour), Directories: map[string]Entry{".": {Size: 1024}, "Growth": {Size: 1024}}}
	latest := &Snapshot{Root: root, Depth: 5, At: time.Now(), Directories: map[string]Entry{".": {Size: 4096}, "Growth": {Size: 4096}}}
	t.Setenv("DISKLENS_DATA", legacy)
	if err := SaveJSON(SnapshotPath(root), &SnapshotState{Version: 1, Baseline: baseline, Latest: latest}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(SnapshotPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyState(legacy, shared); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISKLENS_DATA", shared)
	state, err := LoadSnapshotState(root)
	if err != nil {
		t.Fatal(err)
	}
	tree := &Node{Name: root, Dir: true, Size: 4096}
	child := &Node{Name: "Growth", Dir: true, Size: 4096, Parent: tree}
	tree.setChildren([]*Node{child})
	CompareSaved(tree, state)
	out, err := exec.Command(cli, "diff", root, "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var report DiffReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || !report.BaselineAt.Equal(baseline.At) || !report.ScannedAt.Equal(latest.At) {
		t.Fatalf("migration changed comparison: %+v", report)
	}
	for _, change := range report.Changes {
		if change.DeltaBytes != tree.Delta || change.DeltaBytes != child.Delta {
			t.Fatal("desktop and CLI differ")
		}
	}
	state.Baseline = state.Latest
	if err := SaveJSON(SnapshotPath(root), state); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyState(legacy, shared); err != nil {
		t.Fatal(err)
	}
	current, err := LoadSnapshotState(root)
	if err != nil || len(SnapshotDiff(current).Changes) != 0 {
		t.Fatal("migration overwrote newer shared state", err)
	}
	old, err := os.ReadFile(filepath.Join(legacy, "snapshots", filepath.Base(SnapshotPath(root))))
	if err != nil || !bytes.Equal(old, original) {
		t.Fatal("migration modified original backup", err)
	}
}
