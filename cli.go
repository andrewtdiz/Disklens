package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

func runCLI(args []string, out, errors io.Writer) int {
	jsonOutput := false
	scanning := false
	var positional []string
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		} else {
			positional = append(positional, arg)
		}
	}
	args = positional
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	fail := func(code, root, message string) int {
		if !jsonOutput {
			if root != "" {
				fmt.Fprintf(errors, "%s (%s): %s\n", code, root, message)
			} else {
				fmt.Fprintf(errors, "%s: %s\n", code, message)
			}
			return 1
		}
		_ = encoder.Encode(struct {
			ScanInProgress bool   `json:"scan_in_progress"`
			Status         string `json:"status"`
			Code           string `json:"code"`
			Root           string `json:"root,omitempty"`
			Message        string `json:"message"`
		}{scanning, "error", code, root, message})
		return 1
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") || len(args) == 2 && args[0] == "diff" && (args[1] == "--help" || args[1] == "-h") {
		if !jsonOutput {
			_, err := fmt.Fprintln(out, "Usage: disklens diff [path] [--json]\n\nRead the latest saved comparison; no scanning or writes.\nDefault root: C:\\ on Windows, / on macOS.\nUse --json for exact byte counts and structured output.\nManage scans and baselines in the desktop app.")
			if err != nil {
				return 1
			}
			return 0
		}
		if err := encoder.Encode(map[string]string{"usage": "disklens diff [path] [--json]", "description": "Read the latest saved comparison. Does not scan or write. Manage scans and baselines in the desktop app.", "default_windows": `C:\`, "default_macos": "/"}); err != nil {
			return 1
		}
		return 0
	}
	if len(args) < 1 || len(args) > 2 || args[0] != "diff" {
		return fail("INVALID_ARGUMENTS", "", "Usage: disklens diff [path] [--json]")
	}
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	if len(args) == 2 {
		root = args[1]
		if root == "" || strings.HasPrefix(root, "-") {
			return fail("INVALID_ARGUMENTS", "", "Provide a directory path, or omit it for the default root.")
		}
		if runtime.GOOS == "windows" && len(root) == 2 && root[1] == ':' {
			root += `\`
		}
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return fail("INVALID_PATH", root, err.Error())
	}
	scanning, err = scanInProgress(root)
	if err != nil {
		return fail("STATUS_READ_FAILED", root, err.Error())
	}
	state, err := loadSnapshotState(root)
	if err != nil {
		return fail("STATE_READ_FAILED", root, err.Error())
	}
	if state.Latest == nil {
		if scanning {
			return fail("SCAN_IN_PROGRESS", root, "First scan in progress; no comparison available yet.")
		}
		return fail("NO_SCAN", root, "Scan this root in the Disklens desktop app first.")
	}
	if state.Baseline == nil {
		if scanning {
			return fail("NO_BASELINE", root, "Scan in progress; no baseline selected. Choose Set baseline in the desktop app after the scan completes.")
		}
		return fail("NO_BASELINE", root, "Choose Set baseline in the Disklens desktop app.")
	}
	report := snapshotDiff(state)
	report.ScanInProgress = scanning
	if jsonOutput {
		err = writeDiff(out, report)
	} else {
		err = writeTextDiff(out, report)
	}
	if err != nil {
		return 1
	}
	return 0
}

func writeDiff(out io.Writer, report DiffReport) error {
	header, err := json.MarshalIndent(report.DiffSummary, "", "  ")
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	buffer.Write(bytes.TrimSuffix(header, []byte("\n}")))
	buffer.WriteString(",\n  \"changes\": [")
	quote := func(value string) string { b, _ := json.Marshal(value); return string(b) }
	for i, change := range report.Changes {
		if i > 0 {
			buffer.WriteByte(',')
		}
		fmt.Fprintf(&buffer, "\n    {\"delta\": %s, \"delta_bytes\": %d, \"kind\": %s, \"path\": %s}", quote(change.Delta), change.DeltaBytes, quote(change.Kind), quote(change.Path))
	}
	if len(report.Changes) > 0 {
		buffer.WriteString("\n  ")
	}
	buffer.WriteString("]\n}\n")
	_, err = buffer.WriteTo(out)
	return err
}

func writeTextDiff(out io.Writer, report DiffReport) error {
	var buffer bytes.Buffer
	if report.ScanInProgress {
		buffer.WriteString("Scan in progress - showing the last completed scan.\n")
	}
	fmt.Fprintf(&buffer, "%s - saved comparison\n", report.Root)
	fmt.Fprintf(&buffer, "Baseline:    %s\nLatest scan: %s\n", report.BaselineAt.Local().Format("2006-01-02 15:04:05 -07:00"), report.ScannedAt.Local().Format("2006-01-02 15:04:05 -07:00"))
	fmt.Fprintf(&buffer, "Depth: %d | Allocated disk space\n\n", report.Depth)
	if len(report.Changes) == 0 {
		buffer.WriteString("No size changes.\n")
	} else {
		fmt.Fprintf(&buffer, "%12s  Directory\n", "Change")
		for _, change := range report.Changes {
			fmt.Fprintf(&buffer, "%12s  %s\n", change.Delta, change.Path)
		}
		buffer.WriteString("\n+ increased usage; - freed space. Sizes include descendants; do not sum overlapping rows.\n")
	}
	_, err := buffer.WriteTo(out)
	return err
}
