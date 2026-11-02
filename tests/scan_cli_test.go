package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCLIScanWorkflow(t *testing.T) {
	if os.Getenv("DISKLENS_CLI_EXE") == "" {
		t.Skip("Requires CLI build")
	}
	base := t.TempDir()
	root, data := filepath.Join(base, "files"), filepath.Join(base, "state")
	put(t, root, "parent/growing/cache", 65536)
	put(t, root, "parent/shrinking/cache", 131072)
	put(t, root, "parent/direct", 32768)
	put(t, root, "root-file", 8192)
	first, err := invokeCLI(t, data, "scan", root, "--depth", "2")
	if err != nil || !first.BaselineOnly || first.Depth != 2 || len(first.Changes) != 0 {
		t.Fatalf("automatic baseline: %+v %v", first, err)
	}
	paths, _ := filepath.Glob(filepath.Join(data, "snapshots", "*.json"))
	if len(paths) != 1 {
		t.Fatal("missing saved scan")
	}
	before, _ := os.ReadFile(paths[0])
	put(t, root, "parent/growing/cache", 131072)
	put(t, root, "parent/shrinking/cache", 65536)
	put(t, root, "parent/direct", 65536)
	put(t, root, "root-file", 16384)
	saved := readCLIReport(t, data, root)
	after, _ := os.ReadFile(paths[0])
	if !saved.BaselineOnly || string(before) != string(after) {
		t.Fatal("plain diff mutated data")
	}
	report, err := invokeCLI(t, data, "diff", root, "--refresh")
	if err != nil || report.BaselineOnly || report.BaselineAt != first.BaselineAt || report.Depth != 2 {
		t.Fatalf("refresh: %+v %v", report, err)
	}
	assertChange(t, report, filepath.Join(root, "parent", "growing"), "modified", 65536, 131072)
	assertChange(t, report, filepath.Join(root, "parent", "shrinking"), "modified", 131072, 65536)
	assertChange(t, report, filepath.Join(root, "parent"), "modified", 32768, 65536)
	assertChange(t, report, root, "modified", 8192, 16384)
	var sum int64
	for _, row := range report.Changes {
		sum += row.Delta
		if row.Uncertain {
			t.Fatal("readable contribution marked uncertain")
		}
		if row.Path == root || row.Path == filepath.Join(root, "parent") {
			if row.Scope != "direct_files" {
				t.Fatal("parent overlaps child")
			}
		} else if row.Scope != "subtree" {
			t.Fatal("depth boundary lacks subtree scope")
		}
	}
	if len(report.Changes) != 4 || sum != 40960 || report.NetBytes != sum || report.IncreasedBytes-report.DecreasedBytes != sum {
		t.Fatalf("non-additive contributors: %+v", report)
	}
	deeper, err := invokeCLI(t, data, "scan", root, "--depth", "4")
	if err != nil || deeper.Depth != 2 || deeper.BaselineAt != first.BaselineAt {
		t.Fatalf("common depth: %+v %v", deeper, err)
	}
	reset, err := invokeCLI(t, data, "baseline", root)
	if err != nil || !reset.BaselineOnly || reset.Depth != 4 || len(reset.Changes) != 0 {
		t.Fatalf("reset: %+v %v", reset, err)
	}
	before, _ = os.ReadFile(paths[0])
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	failed, err := invokeCLI(t, data, "scan", root)
	after, _ = os.ReadFile(paths[0])
	if err == nil || failed.Code != "SCAN_FAILED" || string(before) != string(after) {
		t.Fatal("failed scan overwrote saved state")
	}
}

func TestCLICoverageWorkflow(t *testing.T) {
	if os.Getenv("DISKLENS_CLI_EXE") == "" {
		t.Skip("Requires CLI build")
	}
	base := t.TempDir()
	root, data := filepath.Join(base, "files"), filepath.Join(base, "state")
	put(t, root, "blocked/cache", 65536)
	put(t, root, "readable/cache", 65536)
	first, err := invokeCLI(t, data, "scan", root)
	if err != nil || first.Partial {
		t.Fatalf("initial scan: %+v %v", first, err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0700)
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("Requires unprivileged directory permissions")
	}
	put(t, root, "readable/cache", 131072)
	report, err := invokeCLI(t, data, "diff", root, "--refresh")
	if err != nil || !report.Partial || report.LatestUnreadableEntries == 0 {
		t.Fatalf("partial refresh: %+v %v", report, err)
	}
	var uncertain, readable bool
	var sum int64
	for _, row := range report.Changes {
		sum += row.Delta
		if row.Path == blocked {
			uncertain = row.Uncertain
		}
		if row.Path == filepath.Join(root, "readable") {
			readable = !row.Uncertain && row.Delta == 65536
		}
	}
	if !uncertain || !readable || sum != report.NetBytes || report.IncreasedBytes != 65536 || report.DecreasedBytes != 0 || report.UncertainBytes != -65536 {
		t.Fatalf("coverage attribution: %+v", report)
	}
	cmd := exec.Command(os.Getenv("DISKLENS_CLI_EXE"), "coverage", root, "--json")
	cmd.Env = append(os.Environ(), "DISKLENS_DATA="+data)
	output, err := cmd.Output()
	var coverage struct {
		Latest struct {
			Issues           []struct{ Path, Reason string }
			DetailsAvailable bool `json:"details_available"`
		}
	}
	if err != nil || json.Unmarshal(output, &coverage) != nil || !coverage.Latest.DetailsAvailable || len(coverage.Latest.Issues) == 0 || coverage.Latest.Issues[0].Path != blocked || coverage.Latest.Issues[0].Reason == "" {
		t.Fatalf("saved coverage: %s %v", output, err)
	}
	if err := os.Chmod(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	reset, err := invokeCLI(t, data, "baseline", root)
	if err != nil || !reset.Partial || !reset.BaselineOnly {
		t.Fatalf("partial baseline: %+v %v", reset, err)
	}
	restored, err := invokeCLI(t, data, "scan", root)
	if err != nil || !restored.Partial || restored.LatestUnreadableEntries != 0 {
		t.Fatalf("coverage restoration: %+v %v", restored, err)
	}
	for _, row := range restored.Changes {
		if row.Path == blocked && !row.Uncertain {
			t.Fatal("newly readable data labeled confirmed growth")
		}
	}
}
