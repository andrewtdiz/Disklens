//go:build windows && e2e && !cli

package utils

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func deletionFixture(t *testing.T, root, name string, count int) *Node {
	t.Helper()
	target := filepath.Join(root, name)
	for i := 0; i < count; i++ {
		dir := filepath.Join(target, fmt.Sprintf("dir-%03d", i/64))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%05d.bin", i)), make([]byte, 4096), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := walkAffectedDirectory(context.Background(), target)
	if err != nil || s.Errors != 0 {
		t.Fatalf("scan: %v %+v", err, s)
	}
	return s.Root
}

func TestDeletionProgressAndCancellation(t *testing.T) {
	root := t.TempDir()
	n := deletionFixture(t, root, "target", 2048)
	d, err := PrepareFolderDeletion(root, n.FullPath())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var last ScanProgress
	err = d.Remove(ctx, n, func(p ScanProgress) {
		if p.Total <= 0 || p.Completed < last.Completed || p.Completed > p.Total {
			t.Errorf("invalid progress: %+v", p)
		}
		last = p
		if p.Completed > 0 {
			cancel()
		}
	})
	cancel()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	remaining, err := walkAffectedDirectory(context.Background(), n.FullPath())
	if err != nil || remaining.Files == 0 || remaining.Files >= 2048 {
		t.Fatalf("expected partial deletion: %v %+v", err, remaining)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	err = d.Remove(ctx, remaining.Root, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	last = ScanProgress{}
	err = d.Remove(context.Background(), remaining.Root, func(p ScanProgress) { last = p })
	if err != nil || last.Completed != last.Total || last.Fraction != 1 {
		t.Fatalf("completion: %v %+v", err, last)
	}
}

func TestDeletionReadOnlyLateEntriesAndLockedFile(t *testing.T) {
	root := t.TempDir()
	n := deletionFixture(t, root, "target", 1)
	whole, err := walkAffectedDirectory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	n = whole.Root.Children()[0]
	file := n.Children()[0].Children()[0].FullPath()
	if err := os.Chmod(file, 0400); err != nil {
		t.Fatal(err)
	}
	late := filepath.Join(n.FullPath(), "created-after-scan")
	if err := os.WriteFile(late, []byte("late"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := windows.UTF16PtrFromString(file)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := PrepareFolderDeletion(root, n.FullPath())
	if err != nil {
		windows.CloseHandle(h)
		t.Fatal(err)
	}
	defer d.Root.Close()
	err = d.Remove(context.Background(), n, nil)
	windows.CloseHandle(h)
	if err == nil {
		t.Fatal("locked file deletion claimed success")
	}
	if _, err := os.Stat(late); !os.IsNotExist(err) {
		t.Fatal("late entry was not handled")
	}
	r := ReconcileDeletedFolder(n)
	if r.ReconcileErr != nil {
		t.Fatal(r.ReconcileErr)
	}
	ApplyDeletion(whole, r, nil)
	remaining := whole.Root.Children()[0]
	if whole.Files != 1 || remaining.Name != "target" || remaining.FullPath() != filepath.Join(root, "target") || remaining.Children()[0].Children()[0].FullPath() != file {
		t.Fatal("Partial deletion did not preserve reconstructed paths")
	}
	if err := d.Remove(context.Background(), remaining, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionHardLinkReconciliation(t *testing.T) {
	root := t.TempDir()
	n := deletionFixture(t, root, "a-target", 1)
	peerDir := filepath.Join(root, "z-peer")
	if err := os.Mkdir(peerDir, 0700); err != nil {
		t.Fatal(err)
	}
	peerPath := filepath.Join(peerDir, "alias.bin")
	if err := os.Link(n.Children()[0].Children()[0].FullPath(), peerPath); err != nil {
		t.Fatal(err)
	}
	s, err := walkAffectedDirectory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	target := s.Root.Children()[0]
	before := s.Root.Size
	if target.Name != "a-target" || target.Size != before {
		t.Fatal("unexpected allocation owner")
	}
	d, err := PrepareFolderDeletion(root, target.FullPath())
	if err != nil {
		t.Fatal(err)
	}
	err = d.Remove(context.Background(), target, nil)
	d.Root.Close()
	if err != nil {
		t.Fatal(err)
	}
	r := ReconcileDeletedFolder(target)
	if r.ReconcileErr != nil {
		t.Fatal(r.ReconcileErr)
	}
	ApplyDeletion(s, r, nil)
	if s.Root.Size != before || s.Files != 1 || s.Folders != 2 || len(s.Root.Children()) != 1 || s.Root.Children()[0].Size != before {
		t.Fatalf("hard-link totals: %+v root=%+v", s, s.Root)
	}
	fresh, err := walkAffectedDirectory(context.Background(), root)
	if err != nil || fresh.Root.Size != s.Root.Size {
		t.Fatalf("reconciliation differs from fresh scan: %v", err)
	}
}

func TestDeletionBenchmarkFilesystem(t *testing.T) {
	if os.Getenv("DISKLENS_DELETE_BENCH") != "1" {
		t.Skip("set DISKLENS_DELETE_BENCH=1 for disposable deletion measurement")
	}
	root := t.TempDir()
	old := deletionFixture(t, root, "serial", 8192)
	next := deletionFixture(t, root, "parallel", 8192)
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = r.RemoveAll(filepath.Base(old.FullPath()))
	serial := time.Since(start)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	d, err := PrepareFolderDeletion(root, next.FullPath())
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	err = d.Remove(context.Background(), next, nil)
	parallel := time.Since(start)
	d.Root.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("8192 files, 129 folders: serial Root.RemoveAll=%s; retained parallel deletion=%s; ratio=%.2fx", serial, parallel, float64(serial)/float64(parallel))
}
