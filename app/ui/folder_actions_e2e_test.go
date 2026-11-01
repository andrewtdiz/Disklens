//go:build windows && e2e && !cli

package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"disklens/app/utils"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestFolderMenuAndConfirmation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DISKLENS_DATA", t.TempDir())
	target := filepath.Join(root, "Disposable folder")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "data.bin"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	unaffectedPath := filepath.Join(root, "Unaffected folder")
	if err := os.Mkdir(unaffectedPath, 0700); err != nil {
		t.Fatal(err)
	}
	scan, err := utils.WalkDirectory(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.settings.Path = root
	a.scan = scan
	baseline := utils.TakeSnapshot(scan, 5)
	a.savedState = &utils.SnapshotState{Version: 1, Baseline: baseline, Latest: baseline}
	if err := utils.SaveJSON(utils.SnapshotPath(root), a.savedState); err != nil {
		t.Fatal(err)
	}
	var unaffected *utils.Node
	for _, n := range scan.Root.Children() {
		if n.FullPath() == unaffectedPath {
			unaffected = n
		}
	}
	if err := os.WriteFile(filepath.Join(unaffectedPath, "not-rescanned.bin"), make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	a.focus = scan.Root
	a.selected = scan.Root
	a.outline.Open.Add(scan.Root)
	questions := make(chan mygo.MessageOptions, 1)
	replies := make(chan mygo.MessageResult, 1)
	a.confirmDeletion = func(options mygo.MessageOptions) (mygo.MessageResult, error) {
		questions <- options
		return <-replies, nil
	}
	tt := ui.NewTester(a.view, 1280, 860)
	if err := tt.RightClick("Disposable folder"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete permanently..."); err != nil {
		t.Fatal(err)
	}
	var question mygo.MessageOptions
	select {
	case question = <-questions:
	case <-time.After(time.Second):
		t.Fatal("Native dialog was not requested")
	}
	if question.Type != mygo.MessageWarning || !strings.Contains(question.Detail, target) || !strings.Contains(question.Detail, "cannot be undone") || question.DefaultButton != 1 || question.CancelButton != 1 {
		t.Fatal("Native confirmation must show warning, exact path, and default to Cancel")
	}
	tt.Key(ui.Cmd, ui.KeyR)
	if a.busy {
		t.Fatal("Refresh started behind a pending delete confirmation")
	}
	replies <- mygo.MessageResult{Button: 1}
	for deadline := time.Now().Add(time.Second); a.deleteConfirm && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
		tt.Frame()
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("Cancel deleted the fixture")
	}
	if a.deleteRequest != nil {
		t.Fatal("Canceled request retained its directory handle")
	}
	if err := tt.RightClick("Disposable folder"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete permanently..."); err != nil {
		t.Fatal(err)
	}
	select {
	case <-questions:
	case <-time.After(time.Second):
		t.Fatal("Native confirmation missing")
	}
	replies <- mygo.MessageResult{Button: 0}
	for deadline := time.Now().Add(time.Second); a.deleteConfirm && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
		tt.Frame()
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.busy && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
	if a.busy {
		t.Fatal("Delete or post-delete refresh did not finish")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("Confirmed deletion did not remove the fixture")
	}
	if len(unaffected.Children()) != 0 || a.scan.Root.Size != 0 {
		t.Fatal("Deletion rescanned an unrelated folder")
	}
	state, err := utils.LoadSnapshotState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Baseline.Directories["."].Size != baseline.Directories["."].Size || state.Latest.Directories["."].Size != 0 {
		t.Fatal("Deletion did not preserve baseline and save updated totals")
	}
	if _, exists := state.Latest.Directories["Disposable folder"]; exists {
		t.Fatal("Deleted folder remains in saved latest scan")
	}
	if active, err := utils.ScanInProgress(root); err != nil || active {
		t.Fatal("Deletion retained the scan lock", err)
	}
	t.Log("Right-click menu, exact-path confirmation, cancel, confirmed deletion and automatic tree refresh passed")
}
