//go:build !cli

package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"disklens/app/utils"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type deleteConfirmation struct {
	confirmed bool
	err       error
}

func (a *app) folderMenu(c *ui.Context, n *utils.Node, col int) {
	cell := ui.Row(c).Grow(1).AlignItems(ui.Center)
	if col >= 2 {
		cell.Justify(ui.End)
	}
	if n.Dir {
		cell = cell.ContextMenu(func(m *ui.Menu) {
			if m.Item("Properties").Disabled(a.busy || n.Removed).Chosen() {
				a.selected = n
				if err := showFolderProperties(n.FullPath()); err != nil {
					a.status = "Properties: " + err.Error()
				}
			}
			m.Separator()
			if m.Item("Delete permanently...").Disabled(a.busy || n.Removed || n.Parent == nil).Chosen() {
				a.selected = n
				request, err := utils.PrepareFolderDeletion(a.scan.Root.FullPath(), n.FullPath())
				if err != nil {
					a.status = "Delete: " + err.Error()
					return
				}
				request.Node = n
				a.requestDeletion(request)
			}
		})
	}
	cell.Children(func() { a.cell(c, n, col) })
}

func (a *app) requestDeletion(request *utils.FolderDeletion) {
	a.deleteRequest = request
	a.deleteConfirm = true
	a.deleteAnswers = make(chan deleteConfirmation, 1)
	answers := a.deleteAnswers
	show := a.confirmDeletion
	options := mygo.MessageOptions{
		Parent: a.win, Type: mygo.MessageWarning, Title: "Confirm permanent deletion",
		Message: "Delete this folder and all its contents permanently?",
		Detail:  request.Path + "\n\nThis bypasses the Recycle Bin or Trash and cannot be undone.",
		Buttons: []string{"Delete permanently", "Cancel"}, DefaultButton: 1, CancelButton: 1,
	}
	go func() {
		result, err := show(options)
		answers <- deleteConfirmation{confirmed: err == nil && result.Button == 0, err: err}
	}()
}

func (a *app) pollDeleteConfirmation(c *ui.Context) {
	select {
	case answer := <-a.deleteAnswers:
		request := a.deleteRequest
		a.deleteRequest = nil
		a.deleteConfirm = false
		if !answer.confirmed {
			request.Root.Close()
			if answer.err != nil {
				a.status = "Delete: " + answer.err.Error()
			}
			return
		}
		lock, err := utils.BeginScan(a.scan.Root.FullPath())
		if err != nil {
			request.Root.Close()
			a.status = err.Error()
			return
		}
		a.busy, a.deleting = true, true
		a.memory.observe(a.scan != nil, true)
		a.scanStarted = time.Now()
		a.scanProgress = utils.ScanProgress{Phase: "Preparing deletion", Path: request.Path}
		a.deleteResults = make(chan utils.DeletionResult, 1)
		a.progress = make(chan utils.ScanProgress, 1)
		ctx, cancel := context.WithCancel(context.Background())
		a.cancel = cancel
		results, progress := a.deleteResults, a.progress
		go func() {
			defer request.Root.Close()
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
			err := request.Remove(ctx, request.Node, publish)
			publish(utils.ScanProgress{Phase: "Updating affected folder", Path: request.Path})
			result := utils.ReconcileDeletedFolder(request.Node)
			result.Err, result.Lock = err, lock
			results <- result
		}()
	default:
		c.After(100 * time.Millisecond)
	}
}

func (a *app) pollDeletion(c *ui.Context) {
	select {
	case p := <-a.progress:
		a.scanProgress = p
	default:
	}
	select {
	case r := <-a.deleteResults:
		a.cancel()
		a.deleting = false
		if r.ReconcileErr == nil || r.Replacement != nil {
			a.applyDeletion(r)
		} else {
			a.scan.RecordError(r.Target.FullPath(), r.ReconcileErr)
		}
		notice := "Folder deleted"
		if errors.Is(r.Err, context.Canceled) {
			notice = "Deletion canceled · completed deletions cannot be undone"
		} else if r.Err != nil {
			notice = "Deletion incomplete: " + r.Err.Error()
		}
		if r.ReconcileErr != nil {
			notice += " · Refresh needed: " + r.ReconcileErr.Error()
		}
		a.scanProgress = utils.ScanProgress{Phase: "Saving updated sizes", Path: a.scan.Root.FullPath()}
		a.results = make(chan scanResult, 1)
		results, scan, depth := a.results, a.scan, a.settings.Depth
		go func() {
			defer r.Lock.Close()
			result := scanResult{scan: scan, reconcile: true, notice: notice}
			state, err := utils.LoadSnapshotState(scan.Root.FullPath())
			if err == nil && scan.Errors == 0 {
				state.Latest = utils.TakeSnapshot(scan, depth)
				state.Latest.At = time.Now()
				err = utils.SaveJSON(utils.SnapshotPath(scan.Root.FullPath()), state)
				if err == nil {
					result.state, result.saved = state, true
				}
			}
			if err != nil {
				result.notice += fmt.Sprintf(" · Snapshot unchanged: %v", err)
			}
			if scan.Errors != 0 {
				result.notice += " · Snapshot unchanged"
			}
			results <- result
		}()
	default:
		c.After(100 * time.Millisecond)
	}
}
