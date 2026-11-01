//go:build darwin && e2e && !cli

package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMacScannerFilesystem(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3500; i++ {
		path := filepath.Join(root, fmt.Sprintf("folder-%02d", 0), fmt.Sprintf("file-%04d-日本語🙂", i))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, 4097), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(filepath.Join(root, "folder-00", "file-0000-日本語🙂"), filepath.Join(root, "z-hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("ditto", "--hfsCompression", filepath.Join(root, "folder-00", "file-0000-日本語🙂"), filepath.Join(root, "compressed")).CombinedOutput(); err != nil {
		t.Fatalf("compressed fixture: %v %s", err, output)
	}
	for i := 0; i < 64; i++ {
		dir := filepath.Join(root, fmt.Sprintf("branch-%02d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(root, "folder-00", "file-0000-日本語🙂"), filepath.Join(dir, "shared")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "unique"), make([]byte, i*100+1), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, workers := range []int{1, 4, 8} {
		t.Run(fmt.Sprintf("workers-%d", workers), func(t *testing.T) { assertMacParityWorkers(t, root, workers) })
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WalkDirectory(ctx, root, nil); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if _, err := WalkDirectory(ctx, root, func(ScanProgress) { cancel() }); err != context.Canceled {
		t.Fatalf("in-flight cancel: %v", err)
	}
	if _, err := WalkDirectory(context.Background(), filepath.Join(root, "loop"), nil); err == nil {
		t.Fatal("accepted symlink root")
	}
	if err := os.Chmod(filepath.Join(root, "empty"), 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(root, "empty"), 0700)
	partial, err := WalkDirectory(context.Background(), root, nil)
	if err != nil || partial.Errors == 0 {
		t.Fatalf("partial scan: %v, %+v", err, partial)
	}
}

func assertMacParity(t *testing.T, root string) {
	t.Helper()
	assertMacParityWorkers(t, root, 4)
}

func assertMacParityWorkers(t *testing.T, root string, workers int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	portable, err := walkDirectoryPortable(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	portableTime := time.Since(start)
	start = time.Now()
	previous := 0.0
	bulk, err := walkMacDirectoryWorkers(ctx, root, func(p ScanProgress) {
		if p.Fraction < previous || p.Fraction > 1 {
			t.Errorf("progress %v after %v", p.Fraction, previous)
		}
		previous = p.Fraction
	}, workers)
	if err != nil {
		t.Fatal(err)
	}
	bulkTime := time.Since(start)
	if portable.Errors != 0 || bulk.Errors != 0 {
		t.Fatalf("errors: portable %s, bulk %s", portable.FirstError, bulk.FirstError)
	}
	if bulk.Files != portable.Files || bulk.Folders != portable.Folders || bulk.Skipped != portable.Skipped || bulk.Bytes != portable.Bytes || previous != 1 {
		t.Fatalf("totals: portable %+v, bulk %+v", portable, bulk)
	}
	var compare func(*Node, *Node)
	compare = func(a, b *Node) {
		if a.Name != b.Name || a.Size != b.Size || a.Dir != b.Dir || a.Modified != b.Modified || len(a.Children()) != len(b.Children()) {
			t.Fatalf("mismatch %s: %+v / %+v", a.FullPath(), a, b)
		}
		for i := range a.Children() {
			compare(a.Children()[i], b.Children()[i])
		}
	}
	compare(portable.Root, bulk.Root)
	if bulk.FileQueries != 0 {
		t.Fatalf("unexpected file queries: %d", bulk.FileQueries)
	}
	t.Logf("%d files, %d folders, %d bytes: portable %s (%d file queries), bulk %s (%d batches, %d file queries), %.2fx speedup", bulk.Files, bulk.Folders, bulk.Bytes, portableTime, portable.FileQueries, bulkTime, bulk.DirectoryBatches, bulk.FileQueries, float64(portableTime)/float64(bulkTime))
}

func TestMacScannerRealDirectory(t *testing.T) {
	root := os.Getenv("DISKLENS_SCAN_ROOT")
	if root == "" {
		t.Skip("Set DISKLENS_SCAN_ROOT for a real-directory comparison")
	}
	assertMacParity(t, root)
}
