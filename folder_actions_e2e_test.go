//go:build windows && e2e && !cli

package main

import (
	"context"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
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
	err = request.remove()
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
	if err := request.remove(); err == nil {
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
	scan, err := walkWindowsDirectory(context.Background(), root, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.settings.Path = root
	a.scan = scan
	a.focus = scan.Root
	a.selected = scan.Root
	a.outline.Open.Add(scan.Root)
	tt := ui.NewTester(a.view, 1280, 860)
	if err := tt.RightClick("Disposable folder"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete permanently..."); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(target) || !tt.HasText("cannot be undone") {
		t.Fatal("Confirmation does not identify the exact folder and irreversible operation")
	}
	if output := os.Getenv("DISKLENS_DELETE_CAPTURE"); output != "" {
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	tt.Key(ui.Cmd, ui.KeyR)
	if a.busy {
		t.Fatal("Refresh started behind a pending delete confirmation")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
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
	if err := tt.Click("Delete permanently"); err != nil {
		t.Fatal(err)
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
	if len(a.scan.Root.Children) != 0 {
		t.Fatal("Tree was not refreshed after deletion")
	}
	t.Log("Right-click menu, exact-path confirmation, cancel, confirmed deletion and automatic tree refresh passed")
}
