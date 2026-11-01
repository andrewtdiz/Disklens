//go:build !cli

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type deleteConfirmation struct {
	confirmed bool
	err       error
}

type folderDeletion struct {
	root           *os.Root
	relative, path string
	info           os.FileInfo
	node           *Node
}

func prepareFolderDeletion(rootPath, target string) (*folderDeletion, error) {
	rootPath, err := filepath.Abs(filepath.Clean(rootPath))
	if err != nil {
		return nil, err
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootPath, target)
	if err != nil {
		return nil, err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("Choose a folder inside the scanned directory")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat(relative)
	if err != nil {
		root.Close()
		return nil, err
	}
	if !info.IsDir() || skipEntry(info) {
		root.Close()
		return nil, errors.New("The selected path is no longer a regular folder")
	}
	return &folderDeletion{root: root, relative: relative, path: target, info: info}, nil
}

func (a *app) folderMenu(c *ui.Context, n *Node, col int) {
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
				request, err := prepareFolderDeletion(a.scan.Root.FullPath(), n.FullPath())
				if err != nil {
					a.status = "Delete: " + err.Error()
					return
				}
				request.node = n
				a.requestDeletion(request)
			}
		})
	}
	cell.Children(func() { a.cell(c, n, col) })
}

func (a *app) requestDeletion(request *folderDeletion) {
	a.deleteRequest = request
	a.deleteConfirm = true
	a.deleteAnswers = make(chan deleteConfirmation, 1)
	answers := a.deleteAnswers
	show := a.confirmDeletion
	options := mygo.MessageOptions{
		Parent: a.win, Type: mygo.MessageWarning, Title: "Confirm permanent deletion",
		Message: "Delete this folder and all its contents permanently?",
		Detail:  request.path + "\n\nThis bypasses the Recycle Bin or Trash and cannot be undone.",
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
			request.root.Close()
			if answer.err != nil {
				a.status = "Delete: " + answer.err.Error()
			}
			return
		}
		lock, err := beginScan(a.scan.Root.FullPath())
		if err != nil {
			request.root.Close()
			a.status = err.Error()
			return
		}
		a.busy, a.deleting = true, true
		a.memory.observe(a.scan != nil, true)
		a.scanStarted = time.Now()
		a.scanProgress = ScanProgress{Phase: "Preparing deletion", Path: request.path}
		a.deleteResults = make(chan deletionResult, 1)
		a.progress = make(chan ScanProgress, 1)
		ctx, cancel := context.WithCancel(context.Background())
		a.cancel = cancel
		results, progress := a.deleteResults, a.progress
		go func() {
			defer request.root.Close()
			publish := func(p ScanProgress) {
				select {
				case <-progress:
				default:
				}
				select {
				case progress <- p:
				default:
				}
			}
			err := request.remove(ctx, request.node, publish)
			publish(ScanProgress{Phase: "Updating affected folder", Path: request.path})
			result := reconcileDeletedFolder(request.node)
			result.err, result.lock = err, lock
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
		if r.reconcileErr == nil || r.replacement != nil {
			a.applyDeletion(r)
		} else {
			a.scan.recordError(r.target.FullPath(), r.reconcileErr)
		}
		notice := "Folder deleted"
		if errors.Is(r.err, context.Canceled) {
			notice = "Deletion canceled · completed deletions cannot be undone"
		} else if r.err != nil {
			notice = "Deletion incomplete: " + r.err.Error()
		}
		if r.reconcileErr != nil {
			notice += " · Refresh needed: " + r.reconcileErr.Error()
		}
		a.scanProgress = ScanProgress{Phase: "Saving updated sizes", Path: a.scan.Root.FullPath()}
		a.results = make(chan scanResult, 1)
		results, scan, depth := a.results, a.scan, a.settings.Depth
		go func() {
			defer r.lock.Close()
			result := scanResult{scan: scan, reconcile: true, notice: notice}
			state, err := loadSnapshotState(scan.Root.FullPath())
			if err == nil && scan.Errors == 0 {
				state.Latest = takeSnapshot(scan, depth)
				state.Latest.At = time.Now()
				err = saveJSON(snapshotPath(scan.Root.FullPath()), state)
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
