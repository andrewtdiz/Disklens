//go:build e2e && !cli

package utils

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanStatusHolder(t *testing.T) {
	root := os.Getenv("DISKLENS_STATUS_ROOT")
	if root == "" {
		t.Skip("Subprocess helper")
	}
	file, err := BeginScan(root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fmt.Println("ready")
	var signal [1]byte
	_, _ = os.Stdin.Read(signal[:])
}

func TestScanStatusCLIIntegration(t *testing.T) {
	cli := os.Getenv("DISKLENS_CLI_EXE")
	if cli == "" {
		t.Skip("Requires console build")
	}
	data := filepath.Join(t.TempDir(), "state")
	root := filepath.Join(t.TempDir(), "files")
	t.Setenv("DISKLENS_DATA", data)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	holder := func() (*exec.Cmd, func()) {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestScanStatusHolder$")
		cmd.Env = append(os.Environ(), "DISKLENS_STATUS_ROOT="+root)
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		ready, err := bufio.NewReader(output).ReadString('\n')
		if err != nil || strings.TrimSpace(ready) != "ready" {
			t.Fatalf("scan holder failed: %q %v", ready, err)
		}
		return cmd, func() {
			input.Close()
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		}
	}
	query := func() (DiffReport, string, error) {
		cmd := exec.CommandContext(ctx, cli, "diff", root, "--json")
		cmd.Env = os.Environ()
		output, err := cmd.Output()
		var result struct {
			DiffReport
			Code string `json:"code"`
		}
		if parseErr := json.Unmarshal(output, &result); parseErr != nil {
			t.Fatalf("invalid JSON: %s %v", output, parseErr)
		}
		return result.DiffReport, result.Code, err
	}
	_, finish := holder()
	report, code, err := query()
	if err == nil || code != "SCAN_IN_PROGRESS" || !report.ScanInProgress {
		t.Fatalf("first scan status: %+v %s %v", report, code, err)
	}
	if active, err := ScanInProgress(root + "-other"); err != nil || active {
		t.Fatal("status leaked to another root")
	}
	if file, err := BeginScan(root); err == nil {
		file.Close()
		t.Fatal("duplicate scan lock succeeded")
	}
	for _, command := range []string{"scan", "baseline"} {
		cmd := exec.Command(cli, command, root, "--json")
		output, err := cmd.Output()
		var failure struct{ Code string }
		if err == nil || json.Unmarshal(output, &failure) != nil || failure.Code != "SCAN_BUSY" {
			t.Fatalf("concurrent %s not rejected: %s %v", command, output, err)
		}
	}
	old := &Snapshot{Root: root, Depth: 5, At: time.Now().Add(-time.Hour), Directories: map[string]Entry{".": {Size: 4096}}}
	latest := &Snapshot{Root: root, Depth: 5, At: time.Now(), Directories: map[string]Entry{".": {Size: 8192}}}
	state := &SnapshotState{Version: 1, Latest: latest}
	if err := SaveJSON(SnapshotPath(root), state); err != nil {
		t.Fatal(err)
	}
	report, code, err = query()
	if err == nil || code != "NO_BASELINE" || !report.ScanInProgress {
		t.Fatalf("missing baseline while scanning: %+v %s %v", report, code, err)
	}
	state.Baseline = old
	if err := SaveJSON(SnapshotPath(root), state); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(SnapshotPath(root))
	report, code, err = query()
	if err != nil || code != "" || !report.ScanInProgress || len(report.Changes) != 1 || report.Changes[0].DeltaBytes != 4096 {
		t.Fatalf("saved diff during scan: %+v %s %v", report, code, err)
	}
	cmd := exec.CommandContext(ctx, cli, "diff", root)
	text, err := cmd.Output()
	if err != nil || !strings.Contains(string(text), "Scan in progress - showing the last completed scan.") || !strings.Contains(string(text), "+4.00 KiB") {
		t.Fatalf("missing text status: %s %v", text, err)
	}
	after, _ := os.ReadFile(SnapshotPath(root))
	if string(before) != string(after) {
		t.Fatal("CLI changed saved data")
	}
	finish()
	report, _, err = query()
	if err != nil || report.ScanInProgress {
		t.Fatal("scan completion did not clear status")
	}
	crashed, _ := holder()
	if err := crashed.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = crashed.Wait()
	report, _, err = query()
	if err != nil || report.ScanInProgress {
		t.Fatal("crashed scan left stale status")
	}
	t.Log("CLI returns saved diffs immediately during scanning; first-scan, missing-baseline, completion, and crash states verified")
}
