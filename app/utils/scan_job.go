//go:build !cli

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
		if s.Errors > 0 {
			r.Notice = fmt.Sprintf("Partial scan · %d unreadable entries · snapshot unchanged", s.Errors)
		} else if loadErr != nil {
			r.Notice = "Cannot read saved snapshot: " + loadErr.Error()
		} else {
			p := ScanProgress{Phase: "Preparing snapshot", Path: path, Files: s.Files, Folders: s.Folders, Bytes: s.Bytes}
			publish(p)
			next := TakeSnapshot(s, depth)
			next.At = time.Now()
			state.Latest = next
			if ctx.Err() != nil {
				r.Err = ctx.Err()
			} else {
				p.Phase = "Saving scan"
				publish(p)
				if err := SaveJSON(SnapshotPath(path), state); err != nil {
					r.Notice = "Scan could not be saved: " + err.Error()
				} else {
					r.Saved, r.State = true, state
					CompareSaved(s.Root, state)
				}
			}
		}
	}
	return r
}
