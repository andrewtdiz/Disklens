package tests

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type cliChange struct {
	Scope     string `json:"scope"`
	Uncertain bool   `json:"uncertain"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Display   string `json:"delta"`
	Delta     int64  `json:"delta_bytes"`
}

type cliReport struct {
	BaselineOnly              bool        `json:"baseline_only"`
	NetBytes                  int64       `json:"net_bytes"`
	IncreasedBytes            int64       `json:"increased_bytes"`
	DecreasedBytes            int64       `json:"decreased_bytes"`
	UncertainBytes            int64       `json:"uncertain_bytes"`
	Partial                   bool        `json:"partial"`
	BaselineUnreadableEntries int         `json:"baseline_unreadable_entries"`
	LatestUnreadableEntries   int         `json:"latest_unreadable_entries"`
	ScanInProgress            bool        `json:"scan_in_progress"`
	Status                    string      `json:"status"`
	Code                      string      `json:"code"`
	Root                      string      `json:"root"`
	BaselineAt                string      `json:"baseline_at"`
	ScannedAt                 string      `json:"scanned_at"`
	Depth                     int         `json:"depth"`
	SizeBasis                 string      `json:"size_basis"`
	IncludesDescendants       bool        `json:"includes_descendants"`
	Changes                   []cliChange `json:"changes"`
}

func invokeCLI(t *testing.T, data string, args ...string) (cliReport, error) {
	t.Helper()
	exe := os.Getenv("DISKLENS_CLI_EXE")
	if exe == "" {
		t.Fatal("Set DISKLENS_CLI_EXE to the console build")
	}
	cmd := exec.Command(exe, append(args, "--json")...)
	cmd.Env = append(os.Environ(), "DISKLENS_DATA="+data)
	b, err := cmd.Output()
	var report cliReport
	if parseErr := json.Unmarshal(b, &report); parseErr != nil {
		t.Fatalf("CLI did not emit JSON: %q, %v, %v", b, parseErr, err)
	}

	return report, err
}

func readCLIReport(t *testing.T, data, root string) cliReport {
	t.Helper()
	report, err := invokeCLI(t, data, "diff", root)
	if err != nil || report.Status != "ok" {
		t.Fatalf("diff failed: %+v %v", report, err)
	}
	if report.ScanInProgress {
		t.Fatal("completed desktop scan still reported active")
	}
	if report.Changes == nil || report.IncludesDescendants || report.SizeBasis != "allocated_bytes" || report.BaselineAt == "" || report.ScannedAt == "" {
		t.Fatalf("incomplete report: %+v", report)
	}
	magnitude := func(n int64) int64 {
		if n < 0 {
			return -n
		}
		return n
	}
	for i, change := range report.Changes {
		if change.Delta == 0 || change.Display == "" || change.Delta > 0 && !strings.HasPrefix(change.Display, "+") || change.Delta < 0 && !strings.HasPrefix(change.Display, "-") {
			t.Fatalf("invalid delta: %+v", change)
		}
		if i > 0 {
			previous := report.Changes[i-1]
			if change.Uncertain == previous.Uncertain && (change.Delta > 0) == (previous.Delta > 0) && (magnitude(change.Delta) > magnitude(previous.Delta) || magnitude(change.Delta) == magnitude(previous.Delta) && change.Path < previous.Path) {
				t.Fatal("changes not deterministically sorted")
			}
		}
	}
	cmd := exec.Command(os.Getenv("DISKLENS_CLI_EXE"), "diff", root)
	cmd.Env = append(os.Environ(), "DISKLENS_DATA="+data)
	text, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "Baseline:") || !strings.Contains(string(text), "Latest scan:") || json.Valid(text) {
		t.Fatalf("expected readable text: %s", text)
	}
	if len(report.Changes) == 0 && !strings.Contains(string(text), "No measured size changes.") && !strings.Contains(string(text), "Baseline ready") {
		t.Fatalf("missing empty state: %s", text)
	}
	var rows []string
	for _, line := range strings.Split(string(text), "\n") {
		trimmed := strings.TrimSpace(line)
		if (strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-")) && !strings.Contains(line, "increased usage") {
			rows = append(rows, line)
		}
	}
	if len(rows) != len(report.Changes) {
		t.Fatalf("text contains missing or extra changes: %s", text)
	}
	for i, change := range report.Changes {
		if !strings.HasPrefix(rows[i], fmt.Sprintf("%12s  %s [", change.Display, change.Path)) {
			t.Fatalf("text row differs from JSON: %q", rows[i])
		}
	}
	return report
}

func assertCLIError(t *testing.T, data, code string, path ...string) {
	t.Helper()
	report, err := invokeCLI(t, data, append([]string{"diff"}, path...)...)
	if err == nil || report.Status != "error" || report.Code != code {
		t.Fatalf("want %s and nonzero exit: %+v %v", code, report, err)
	}
}

func assertChange(t *testing.T, report cliReport, path, kind string, before, after int64) {
	t.Helper()
	for _, change := range report.Changes {
		if change.Path == path {
			if change.Kind != kind || change.Delta != after-before {
				t.Fatalf("wrong change: %+v", change)
			}
			return
		}
	}
	t.Fatalf("missing changed directory %s: %+v", path, report)
}

func savedLatest(t *testing.T, b []byte) json.RawMessage {
	t.Helper()
	var state struct {
		Version int
		Latest  json.RawMessage
	}
	if err := json.Unmarshal(b, &state); err != nil || state.Version != 1 || len(state.Latest) == 0 {
		t.Fatalf("invalid scan state: %s", b)
	}
	return state.Latest
}

func TestCLIReadOnlyErrorsAndDefault(t *testing.T) {
	if os.Getenv("DISKLENS_CLI_EXE") == "" {
		t.Skip("Requires console build")
	}
	data := filepath.Join(t.TempDir(), "state")
	assertCLIError(t, data, "NO_SCAN")
	cmd := exec.Command(os.Getenv("DISKLENS_CLI_EXE"), "diff")
	cmd.Env = append(os.Environ(), "DISKLENS_DATA="+data)
	text, textErr := cmd.CombinedOutput()
	if textErr == nil || !strings.Contains(string(text), "NO_SCAN") || json.Valid(text) {
		t.Fatalf("missing readable error: %s %v", text, textErr)
	}
	cmd = exec.Command(os.Getenv("DISKLENS_CLI_EXE"), "--help")
	text, textErr = cmd.Output()
	if textErr != nil || !strings.Contains(string(text), "--json") || json.Valid(text) {
		t.Fatalf("missing readable help: %s %v", text, textErr)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("read-only CLI created state directory")
	}
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	report, _ := invokeCLI(t, data, "diff")
	if report.Root != root {
		t.Fatalf("wrong default root: %q", report.Root)
	}
	report, err := invokeCLI(t, data, "unknown-command")
	if err == nil || report.Code != "INVALID_ARGUMENTS" {
		t.Fatal("unexpected mutating command")
	}
	key := root
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	statePath := filepath.Join(data, "snapshots", fmt.Sprintf("%x.json", sha256.Sum256([]byte(key))))
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal(map[string]any{"root": root, "depth": 5, "at": "2026-10-30T20:00:00Z", "directories": map[string]any{".": map[string]int{"size": 4096}}})
	if err := os.WriteFile(statePath, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	assertCLIError(t, data, "NO_BASELINE")
	after, _ := os.ReadFile(statePath)
	if string(after) != string(legacy) {
		t.Fatal("CLI rewrote legacy state")
	}
	if err := os.WriteFile(statePath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	assertCLIError(t, data, "STATE_READ_FAILED")
}
