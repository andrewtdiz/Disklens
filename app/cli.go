package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"disklens/app/utils"
)

const cliUsage = `Usage:
  disklens scan [path] [--depth 1-50] [--json]
  disklens diff [path] [--refresh] [--depth 1-50] [--json]
  disklens baseline [path] [--json]
  disklens coverage [path] [--json]

The first saved scan becomes the baseline. Later scans compare against it.
Plain diff reads saved data; --refresh scans first. baseline replaces the baseline
with the latest saved scan. coverage lists unreadable paths in both snapshots.
Default root: C:\ on Windows, / on macOS. Default snapshot depth: 5.
Contributors do not overlap; subtree rows include detail below saved depth.
Partial scans succeed with coverage warnings. Errors exit 1. Progress uses stderr.`

func runCLI(args []string, out, errors io.Writer) int {
	jsonOutput, refresh, help := false, false, false
	depth := 0
	var positional []string
	argumentError := ""
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		}
	}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--json":
		case "--refresh":
			refresh = true
		case "--help", "-h":
			help = true
		case "--depth":
			i++
			if i == len(args) {
				argumentError = "--depth needs a value from 1 to 50"
				break
			}
			value, err := strconv.Atoi(args[i])
			if err != nil || value < 1 || value > 50 {
				argumentError = "--depth must be between 1 and 50"
			} else {
				depth = value
			}
		default:
			if strings.HasPrefix(arg, "-") {
				argumentError = "Unknown option: " + arg
			} else {
				positional = append(positional, arg)
			}
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	scanning := false
	fail := func(code, root, message string) int {
		if jsonOutput {
			_ = encoder.Encode(map[string]any{"scan_in_progress": scanning, "status": "error", "code": code, "root": root, "message": message})
		} else {
			fmt.Fprintf(errors, "%s: %s\n", code, message)
		}
		return 1
	}
	if argumentError != "" {
		return fail("INVALID_ARGUMENTS", "", argumentError)
	}
	if help {
		var err error
		if jsonOutput {
			err = encoder.Encode(map[string]string{"usage": cliUsage})
		} else {
			_, err = fmt.Fprintln(out, cliUsage)
		}
		if err != nil {
			return 1
		}
		return 0
	}
	if len(positional) < 1 || len(positional) > 2 {
		return fail("INVALID_ARGUMENTS", "", cliUsage)
	}
	command := positional[0]
	if command != "scan" && command != "diff" && command != "baseline" && command != "coverage" {
		return fail("INVALID_ARGUMENTS", "", cliUsage)
	}
	if refresh && command != "diff" || depth != 0 && command != "scan" && !(command == "diff" && refresh) {
		return fail("INVALID_ARGUMENTS", "", "--refresh is for diff; --depth requires scan or diff --refresh")
	}
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	if len(positional) == 2 {
		root = positional[1]
	}
	if root == "" {
		return fail("INVALID_PATH", root, "Provide a directory path")
	}
	if runtime.GOOS == "windows" && len(root) == 2 && root[1] == ':' {
		root += `\`
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return fail("INVALID_PATH", root, err.Error())
	}
	writing := command == "scan" || refresh || command == "baseline"
	if writing {
		if err := utils.MigrateState(); err != nil {
			return fail("STATE_MIGRATION_FAILED", root, err.Error())
		}
		lock, err := utils.BeginScan(root)
		if err != nil {
			scanning, _ = utils.ScanInProgress(root)
			return fail("SCAN_BUSY", root, err.Error())
		}
		defer lock.Close()
	} else {
		scanning, err = utils.ScanInProgress(root)
		if err != nil {
			return fail("STATUS_READ_FAILED", root, err.Error())
		}
	}
	state, err := utils.LoadSnapshotState(root)
	if err != nil {
		return fail("STATE_READ_FAILED", root, err.Error())
	}
	if command == "scan" || refresh {
		if depth == 0 {
			depth = 5
			if state.Latest != nil {
				depth = state.Latest.Depth
			}
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		lastProgress := time.Time{}
		result := utils.ScanAndSave(ctx, root, depth, func(p utils.ScanProgress) {
			if time.Since(lastProgress) >= time.Second {
				fmt.Fprintf(errors, "%s · %d files · %s\n", p.Phase, p.Files, utils.FormatSize(p.Bytes))
				lastProgress = time.Now()
			}
		})
		if result.Err != nil {
			return fail("SCAN_FAILED", root, result.Err.Error())
		}
		if !result.Saved {
			return fail("SAVE_FAILED", root, result.Notice)
		}
		state = result.State
	}
	if state.Latest == nil {
		if scanning {
			return fail("SCAN_IN_PROGRESS", root, "First scan in progress; no saved comparison yet")
		}
		return fail("NO_SCAN", root, "Run disklens scan "+strconv.Quote(root)+" first")
	}
	if command == "coverage" {
		if jsonOutput {
			if err := encoder.Encode(map[string]any{"root": root, "scan_in_progress": scanning, "baseline": coverageSnapshot(state.Baseline), "latest": coverageSnapshot(state.Latest)}); err != nil {
				return 1
			}
		} else {
			for _, item := range []struct {
				name     string
				snapshot *utils.Snapshot
			}{{"Baseline", state.Baseline}, {"Latest", state.Latest}} {
				if item.snapshot == nil {
					continue
				}
				fmt.Fprintf(out, "%s: %s · %d unreadable entries\n", item.name, item.snapshot.At.Format(time.RFC3339), item.snapshot.UnreadableEntries)
				if item.snapshot.UnreadableEntries > len(item.snapshot.Issues) {
					fmt.Fprintln(out, "Some paths were not retained by this older snapshot; rescan for coverage details.")
				}
				for _, issue := range item.snapshot.Issues {
					fmt.Fprintf(out, "%s: %s\n", issue.Path, issue.Reason)
				}
			}
		}
		return 0
	}
	if command == "baseline" {
		state.Baseline = state.Latest
		if err := utils.SaveJSON(utils.SnapshotPath(root), state); err != nil {
			return fail("SAVE_FAILED", root, err.Error())
		}
	}
	if state.Baseline == nil {
		return fail("NO_BASELINE", root, "Run disklens baseline "+strconv.Quote(root)+" to use the saved scan as the baseline")
	}
	report := utils.SnapshotDiff(state)
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

func coverageSnapshot(snapshot *utils.Snapshot) any {
	if snapshot == nil {
		return nil
	}
	return map[string]any{"scanned_at": snapshot.At, "unreadable_entries": snapshot.UnreadableEntries, "details_available": snapshot.UnreadableEntries == len(snapshot.Issues), "issues": snapshot.Issues}
}

func writeDiff(out io.Writer, report utils.DiffReport) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func writeTextDiff(out io.Writer, report utils.DiffReport) error {
	var buffer strings.Builder
	if report.ScanInProgress {
		buffer.WriteString("Scan in progress - showing the last completed scan.\n")
	}
	fmt.Fprintf(&buffer, "%s\nBaseline: %s · Latest scan: %s\n", report.Root, report.BaselineAt.Local().Format("Jan 2 15:04"), report.ScannedAt.Local().Format("Jan 2 15:04"))
	if report.Partial {
		fmt.Fprintf(&buffer, "Partial comparison: %d unreadable (baseline %d). ? = uncertain.\n", report.LatestUnreadableEntries, report.BaselineUnreadableEntries)
	}
	if report.BaselineOnly {
		buffer.WriteString("Baseline ready — scan again to measure changes.\n")
	} else {
		fmt.Fprintf(&buffer, "Net change: %s\n\n", utils.SignedSize(report.NetBytes))
		if len(report.Changes) == 0 {
			buffer.WriteString("No measured size changes.\n")
		}
		for _, change := range report.Changes {
			scope := "files"
			if change.Scope == "subtree" {
				scope = "subtree"
			}
			if change.Uncertain {
				scope += " ?"
			}
			fmt.Fprintf(&buffer, "%12s  %s [%s]\n", change.Delta, change.Path, scope)
		}
	}
	_, err := io.WriteString(out, buffer.String())
	return err
}
