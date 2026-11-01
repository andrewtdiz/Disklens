//go:build !cli

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"disklens/app/utils"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type scanResult struct {
	scan        *utils.Scan
	state       *utils.SnapshotState
	reset       bool
	reconcile   bool
	err         error
	notice      string
	saved       bool
	cleanupDone <-chan struct{}
}

type app struct {
	memory                  idleMemory
	confirmDeletion         func(mygo.MessageOptions) (mygo.MessageResult, error)
	deleteAnswers           chan deleteConfirmation
	deleteRequest           *utils.FolderDeletion
	deleteConfirm, deleting bool
	deleteResults           chan utils.DeletionResult
	win                     *mygo.Window
	settings                utils.Settings
	depthText               string
	depth                   float64
	split                   float32
	scan                    *utils.Scan
	focus                   *utils.Node
	selected                *utils.Node
	hover                   *utils.Node
	outline                 ui.OutlineState[*utils.Node]
	row                     int
	cleanupPending          bool
	cleanupDone             <-chan struct{}
	busy                    bool
	cancel                  context.CancelFunc
	results                 chan scanResult
	progress                chan utils.ScanProgress
	scanProgress            utils.ScanProgress
	scanStarted             time.Time
	status                  string
	savedState              *utils.SnapshotState
	stateMigrationErr       error
	baseline                string
	tiles                   []tile
	mapRoot                 *utils.Node
	mapColorRoot            *utils.Node
	mapColors               map[string]ui.Color
	mapWidth, mapHeight     float32
	selectionTrail          []*utils.Node
	wheelRemainder          float32
	mapBitmap               *ui.Bitmap
}

func newApp() *app {
	home, _ := os.UserHomeDir()
	a := &app{confirmDeletion: mygo.Dialog.Message, settings: utils.Settings{Path: home, Depth: 5}, split: 50, row: 0, status: "Choose a folder, then Refresh to scan."}
	if err := utils.MigrateState(); err != nil {
		a.stateMigrationErr = err
		a.status = "Could not migrate saved scans: " + err.Error()
	}
	if b, err := os.ReadFile(filepath.Join(utils.DataDirectory(), "settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
	if a.settings.Depth < 1 || a.settings.Depth > 50 {
		a.settings.Depth = 5
	}
	a.depth = float64(a.settings.Depth)
	a.depthText = strconv.Itoa(a.settings.Depth)
	a.outline.List.Selected = &a.row
	return a
}

func (a *app) start() {
	if a.stateMigrationErr != nil {
		a.status = "Could not migrate saved scans: " + a.stateMigrationErr.Error()
		return
	}
	if a.busy || a.deleteConfirm {
		return
	}
	path, err := filepath.Abs(filepath.Clean(a.settings.Path))
	if err != nil {
		a.status = err.Error()
		return
	}
	scanLock, err := utils.BeginScan(path)
	if err != nil {
		a.status = err.Error()
		return
	}
	a.settings.Path = path
	a.settings.Depth = int(a.depth)
	settingsErr := utils.SaveJSON(filepath.Join(utils.DataDirectory(), "settings.json"), a.settings)
	a.busy = true
	a.memory.observe(a.scan != nil, true)
	a.scanStarted = time.Now()
	a.scanProgress = utils.ScanProgress{Phase: "Starting scan", Path: path}
	a.status = "Scanning…"
	a.results, a.progress = make(chan scanResult, 1), make(chan utils.ScanProgress, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	results, progress, depth := a.results, a.progress, a.settings.Depth
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		defer debug.FreeOSMemory()
		defer scanLock.Close()
		publish := func(p utils.ScanProgress) {
			select {
			case <-progress:
			default:
			}
			select {
			case progress <- p:
			default:
			}
		}
		outcome := utils.ScanAndSave(ctx, path, depth, publish)
		r := scanResult{scan: outcome.Scan, state: outcome.State, saved: outcome.Saved, notice: outcome.Notice, err: outcome.Err, cleanupDone: cleanupDone}
		if outcome.Err == nil && settingsErr != nil {
			r.notice += " · Settings could not be saved: " + settingsErr.Error()
		}
		results <- r
	}()
}

func (a *app) setBaseline() {
	if a.busy || a.savedState == nil || a.scan == nil {
		return
	}
	state := &utils.SnapshotState{Version: 1, Latest: a.savedState.Latest, Baseline: a.savedState.Latest}
	a.busy = true
	a.memory.observe(a.scan != nil, true)
	a.scanStarted = time.Now()
	a.scanProgress = utils.ScanProgress{Phase: "Setting baseline", Path: state.Latest.Root}
	a.results, a.progress = make(chan scanResult, 1), make(chan utils.ScanProgress, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	results, scan := a.results, a.scan
	go func() {
		err := ctx.Err()
		if err == nil {
			err = utils.SaveJSON(utils.SnapshotPath(state.Latest.Root), state)
		}
		results <- scanResult{scan: scan, state: state, saved: err == nil, reset: true, err: err}
	}()
}

func (a *app) poll(c *ui.Context) {
	if a.deleteConfirm {
		a.pollDeleteConfirmation(c)
		return
	}
	if a.deleting {
		a.pollDeletion(c)
		return
	}
	if !a.busy {
		return
	}
	select {
	case n := <-a.progress:
		a.scanProgress = n
	default:
	}
	select {
	case r := <-a.results:
		a.cleanupPending = true
		a.cleanupDone = r.cleanupDone
		a.busy = false
		a.cancel()
		if r.err != nil {
			if r.err == context.Canceled {
				a.status = "Scan canceled · previous scan and snapshot retained"
			} else {
				a.status = r.err.Error()
			}
			return
		}
		if !r.reconcile {
			a.scan, a.focus, a.selected = r.scan, r.scan.Root, r.scan.Root
			a.hover, a.mapRoot = nil, nil
			a.mapColorRoot, a.mapBitmap, a.tiles, a.mapColors = nil, nil, nil, nil
			a.selectionTrail = nil
			a.outline = ui.OutlineState[*utils.Node]{}
			a.row = 0
			a.outline.List.Selected = &a.row
			a.outline.Open.Add(a.scan.Root)
		}
		a.savedState = r.state
		a.baseline = "Scan not saved"
		if r.saved {
			a.baseline = "No baseline - scan saved" + fmt.Sprintf(" - depth %d", r.state.Latest.Depth)
		}
		if r.state != nil && r.state.Baseline != nil {
			a.baseline = "Compared with " + r.state.Baseline.At.Format("Jan 2, 15:04:05") + fmt.Sprintf(" - depth %d", min(r.state.Baseline.Depth, r.state.Latest.Depth))
			a.baseline += " · scan " + r.state.Latest.At.Format("Jan 2, 15:04:05")
		}
		if r.reset || r.reconcile {
			utils.ClearComparison(a.scan.Root)
			if r.state != nil {
				utils.CompareSaved(a.scan.Root, r.state)
			}
			if r.reset {
				a.baseline = "Baseline set " + r.state.Baseline.At.Format("Jan 2, 15:04:05") + fmt.Sprintf(" - depth %d", r.state.Baseline.Depth)
			}
		}
		if r.state != nil && r.state.Baseline != nil && r.state.Baseline.UnreadableEntries > 0 {
			a.baseline += fmt.Sprintf(" · Partial baseline (%d unreadable entries)", r.state.Baseline.UnreadableEntries)
		}
		if r.scan.Errors > 0 {
			a.baseline += fmt.Sprintf(" · Partial scan (%d unreadable entries)", r.scan.Errors)
		}
		a.status = fmt.Sprintf("%s on disk · %d files · %d folders · %d links / special entries skipped", utils.FormatSize(a.scan.Root.Size), a.scan.Files, a.scan.Folders, a.scan.Skipped)
		if r.notice != "" {
			a.status = r.notice
		}
		if !r.reset {
			a.status += " · " + time.Since(a.scanStarted).Round(100*time.Millisecond).String()
		}
	default:
		c.After(100 * time.Millisecond)
	}
}

func (a *app) selectNode(n *utils.Node) {
	a.selected = n
	a.selectionTrail = nil
	for p := n.Parent; p != nil; p = p.Parent {
		a.outline.Open.Add(p)
	}
	var find func(*utils.Node) bool
	row := 0
	find = func(p *utils.Node) bool {
		if p == n {
			a.row = row
			a.outline.List.ScrollTo(row, ui.Center)
			return true
		}
		row++
		if a.outline.Open.Has(p) {
			for _, ch := range p.Children() {
				if find(ch) {
					return true
				}
			}
		}
		return false
	}
	find(a.scan.Root)
}
