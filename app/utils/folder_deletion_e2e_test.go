//go:build windows && e2e && !cli

package utils

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	request, err := PrepareFolderDeletion(root, target)
	if err != nil {
		t.Fatal(err)
	}
	request.Root.Close()
	if _, err := os.Stat(target); err != nil {
		t.Fatal("Preparing or canceling a deletion must not delete anything")
	}
	request, err = PrepareFolderDeletion(root, target)
	if err != nil {
		t.Fatal(err)
	}
	scan, scanErr := walkAffectedDirectory(context.Background(), target)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	err = request.Remove(context.Background(), scan.Root, nil)
	request.Root.Close()
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
		request, err := PrepareFolderDeletion(root, path)
		if err == nil {
			request.Root.Close()
			t.Fatalf("Accepted out-of-scope target %s", path)
		}
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	request, err = PrepareFolderDeletion(root, target)
	if err != nil {
		t.Fatal(err)
	}
	defer request.Root.Close()
	if err := os.Rename(target, target+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := request.Remove(context.Background(), &Node{Name: target, Dir: true}, nil); err == nil {
		t.Fatal("Deleted a replacement folder that was not confirmed")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	t.Log("Permanent deletion, cancel-without-deletion, junction isolation, root/outside rejection, and changed-folder refusal passed")
}
