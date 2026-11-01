//go:build darwin && e2e && !cli

package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMacDeletionHardLinkReconciliation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DISKLENS_DATA", t.TempDir())
	for _, name := range []string{"a-target", "z-peer"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	original := filepath.Join(root, "a-target", "file")
	if err := os.WriteFile(original, make([]byte, 8193), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, filepath.Join(root, "z-peer", "alias")); err != nil {
		t.Fatal(err)
	}
	s, err := walkAffectedDirectory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Errors != 0 {
		t.Fatal(s.FirstError)
	}
	target := s.Root.Children()[0]
	before := s.Bytes
	if target.Name != "a-target" || target.Size != before {
		t.Fatal("unexpected allocation owner")
	}
	owner := target.Children()[0]
	peer := s.Root.Children()[1].Children()[0]
	if owner.Links == nil || owner.Links != peer.Links || len(owner.Links.Nodes) != 2 {
		t.Fatal("hard-link relationships were not retained")
	}
	d, err := PrepareFolderDeletion(root, target.FullPath())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Root.Close()
	if err := d.Remove(context.Background(), target, nil); err != nil {
		t.Fatal(err)
	}
	result := ReconcileDeletedFolder(target)
	if result.ReconcileErr != nil {
		t.Fatal(result.ReconcileErr)
	}
	ApplyDeletion(s, result, nil)
	fresh, err := walkAffectedDirectory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Bytes != before || s.Bytes != fresh.Bytes || s.Files != 1 || s.Folders != 2 || peer.Size != before {
		t.Fatalf("allocation lost after deletion: %+v", s)
	}
}
