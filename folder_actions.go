//go:build !cli

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type folderDeletion struct {
	root           *os.Root
	relative, path string
	info           os.FileInfo
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

func (d *folderDeletion) remove() error {
	info, err := d.root.Lstat(d.relative)
	if err != nil {
		return err
	}
	if !os.SameFile(d.info, info) || !info.IsDir() || skipEntry(info) {
		return errors.New("The folder changed after confirmation; nothing was deleted")
	}
	return d.root.RemoveAll(d.relative)
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
				if err := showFolderProperties(n.Path); err != nil {
					a.status = "Properties: " + err.Error()
				}
			}
			m.Separator()
			if m.Item("Delete permanently...").Disabled(a.busy || n.Removed || n.Parent == nil).Chosen() {
				a.selected = n
				request, err := prepareFolderDeletion(a.scan.Root.Path, n.Path)
				if err != nil {
					a.status = "Delete: " + err.Error()
					return
				}
				a.deleteRequest = request
				a.deleteConfirm = true
			}
		})
	}
	cell.Children(func() { a.cell(c, n, col) })
}

func (a *app) deletionDialog(c *ui.Context) {
	if a.deleteRequest == nil {
		return
	}
	ui.Modal(c.Key("delete-confirmation"), &a.deleteConfirm, func() {
		ui.Column(c).Width(500).Padding(20).Gap(14).Children(func() {
			ui.Text(c, "Delete folder permanently?").FontSize(17)
			ui.Text(c, a.deleteRequest.path).FontSize(12)
			ui.Text(c, "This deletes the folder and everything inside it without using the Recycle Bin or Trash. This cannot be undone.").FontSize(12)
			ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("delete-cancel"), "Cancel").Clicked() {
					a.deleteConfirm = false
				}
				if ui.Button(c.Key("delete-permanently"), "Delete permanently").Background(ui.Hex("#b4232c")).TextColor(ui.Hex("#ffffff")).Clicked() {
					request := a.deleteRequest
					a.deleteRequest = nil
					a.deleteConfirm = false
					a.busy, a.deleting = true, true
					a.scanStarted = time.Now()
					a.scanProgress = ScanProgress{Phase: "Deleting folder", Path: request.path}
					a.deleteResults = make(chan error, 1)
					results := a.deleteResults
					go func() { defer request.root.Close(); results <- request.remove() }()
				}
			})
		})
	})
	if !a.deleteConfirm && a.deleteRequest != nil {
		a.deleteRequest.root.Close()
		a.deleteRequest = nil
	}
}

func (a *app) pollDeletion(c *ui.Context) {
	select {
	case err := <-a.deleteResults:
		a.busy, a.deleting = false, false
		a.settings.Path = a.scan.Root.Path
		if err != nil {
			_, _ = mygo.Dialog.Message(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageError, Title: "Could not finish deleting folder", Message: err.Error(), Detail: "Some contents may have been deleted. The directory will be rescanned."})
		}
		a.start()
	default:
		c.After(100 * time.Millisecond)
	}
}
