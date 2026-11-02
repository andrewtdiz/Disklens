package utils

import (
	"context"
	"fmt"
	"time"
)

type ScanOutcome struct {
	Scan   *Scan
	State  *SnapshotState
	Saved  bool
	Notice string
	Err    error
}

func ScanAndSave(ctx context.Context, path string, depth int, publish func(ScanProgress)) ScanOutcome {
	state, loadErr := LoadSnapshotState(path)
	s, err := WalkDirectory(ctx, path, publish)
	if err == nil {
		err = ctx.Err()
	}
	r := ScanOutcome{Scan: s, Err: err}
	if err == nil {
		if loadErr != nil {
			r.Notice = "Cannot read saved snapshot: " + loadErr.Error()
		} else {
			p := ScanProgress{Phase: "Preparing snapshot", Path: path, Files: s.Files, Folders: s.Folders, Bytes: s.Bytes}
			publish(p)
			next := TakeSnapshot(s, depth)
			next.At = time.Now()
			if state.Baseline == nil && state.Latest != nil {
				state.Baseline = state.Latest
				r.Notice = "Previous saved scan selected as baseline"
			}
			state.Latest = next
			if state.Baseline == nil {
				state.Baseline = next
				r.Notice = "Baseline created — scan again to measure changes"
			}
			if ctx.Err() != nil {
				r.Err = ctx.Err()
			} else {
				p.Phase = "Saving scan"
				publish(p)
				if err := SaveJSON(SnapshotPath(path), state); err != nil {
					r.Notice = "Scan could not be saved: " + err.Error()
				} else {
					r.Saved, r.State = true, state
					if s.Errors > 0 {
						if r.Notice != "" {
							r.Notice += " · "
						}
						r.Notice += fmt.Sprintf("Partial scan saved · %d unreadable entries", s.Errors)
					}
					CompareSaved(s.Root, state)
				}
			}
		}
	}
	return r
}
