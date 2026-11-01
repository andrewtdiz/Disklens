package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopPartialBaseline(t *testing.T) {
	exe := os.Getenv("DISKLENS_EXE")
	if exe == "" {
		t.Skip("Requires desktop build")
	}
	base := t.TempDir()
	root, data := filepath.Join(base, "files"), filepath.Join(base, "state")
	put(t, root, "readable/data", 8192)
	put(t, root, "blocked/data", 4096)
	blocked := filepath.Join(root, "blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0700)
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("Requires unprivileged filesystem permissions")
	}
	c, process := launch(t, exe, data, filepath.Join(base, "first.sock"))
	c.value("path", root)
	c.press("refresh")
	c.waitFor("Partial scan saved")
	c.press("set-baseline")
	c.waitFor("Partial baseline (1 unreadable entries)")
	check := func(wantLatest, wantBaseline int) {
		t.Helper()
		paths, _ := filepath.Glob(filepath.Join(data, "snapshots", "*.json"))
		if len(paths) != 1 {
			t.Fatal("missing snapshot")
		}
		b, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Latest, Baseline struct {
				Unreadable int `json:"unreadable_entries"`
			}
		}
		if err := json.Unmarshal(b, &state); err != nil {
			t.Fatal(err)
		}
		if state.Latest.Unreadable != wantLatest || state.Baseline.Unreadable != wantBaseline {
			t.Fatalf("incorrect saved counts: %s", b)
		}
		report := readCLIReport(t, data, root)
		if report.Partial != (wantLatest+wantBaseline > 0) || report.LatestUnreadableEntries != wantLatest || report.BaselineUnreadableEntries != wantBaseline {
			t.Fatalf("incorrect CLI partial state: %+v", report)
		}
	}
	check(1, 1)
	cmd := exec.Command(os.Getenv("DISKLENS_CLI_EXE"), "diff", root)
	cmd.Env = append(os.Environ(), "DISKLENS_DATA="+data)
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "Partial comparison") {
		t.Fatalf("missing text warning: %s %v", out, err)
	}
	process.Process.Kill()
	process.Wait()
	c.conn.Close()
	check(1, 1)
	if err := os.Chmod(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	c, _ = launch(t, exe, data, filepath.Join(base, "second.sock"))
	c.value("path", root)
	c.press("refresh")
	c.waitFor("Partial baseline (1 unreadable entries)")
	check(0, 1)
	c.press("set-baseline")
	c.waitFor("Baseline set")
	check(0, 0)
}
