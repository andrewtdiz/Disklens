//go:build windows && e2e && !cli

package main

import (
	"context"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFolderDeletionFilesystem(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "delete me")
	nested := filepath.Join(target, "nested")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "data.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(target, "external link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Fatalf("junction: %v %s", err, out)
	}
	request, err := prepareFolderDeletion(root, target)
	if err != nil {
		t.Fatal(err)
	}
	request.root.Close()
	if _, err := os.Stat(target); err != nil {
		t.Fatal("Preparing or canceling a deletion must not delete anything")
	}
	request, err = prepareFolderDeletion(root, target)
	if err != nil {
		t.Fatal(err)
	}
	scan, scanErr := walkAffectedDirectory(context.Background(), target)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	err = request.remove(context.Background(), scan.Root, nil)
	request.root.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("Selected fixture folder was not removed")
	}
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
		t.Fatal("Deletion followed the external junction")
	}
	for _, path := range []string{root, outside, filepath.Join(root, "..", filepath.Base(outside))} {
		request, err := prepareFolderDeletion(root, path)
		if err == nil {
			request.root.Close()
			t.Fatalf("Accepted out-of-scope target %s", path)
		}
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	request, err = prepareFolderDeletion(root, target)
	if err != nil {
		t.Fatal(err)
	}
	defer request.root.Close()
	if err := os.Rename(target, target+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := request.remove(context.Background(), &Node{Name: target, Dir: true}, nil); err == nil {
		t.Fatal("Deleted a replacement folder that was not confirmed")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	t.Log("Permanent deletion, cancel-without-deletion, junction isolation, root/outside rejection, and changed-folder refusal passed")
}

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
	scan, err := walkWindowsDirectory(context.Background(), root, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.settings.Path = root
	a.scan = scan
	baseline := takeSnapshot(scan, 5)
	a.savedState = &SnapshotState{Version: 1, Baseline: baseline, Latest: baseline}
	if err := saveJSON(snapshotPath(root), a.savedState); err != nil {
		t.Fatal(err)
	}
	var unaffected *Node
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
	state, err := loadSnapshotState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Baseline.Directories["."].Size != baseline.Directories["."].Size || state.Latest.Directories["."].Size != 0 {
		t.Fatal("Deletion did not preserve baseline and save updated totals")
	}
	if _, exists := state.Latest.Directories["Disposable folder"]; exists {
		t.Fatal("Deleted folder remains in saved latest scan")
	}
	if active, err := scanInProgress(root); err != nil || active {
		t.Fatal("Deletion retained the scan lock", err)
	}
	t.Log("Right-click menu, exact-path confirmation, cancel, confirmed deletion and automatic tree refresh passed")
}
