//go:build windows && e2e && !cli

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestScannerFilesystem(t *testing.T) {
	root := os.Getenv("DISKLENS_SCAN_ROOT")
	if root == "" {
		t.Skip("Set DISKLENS_SCAN_ROOT to benchmark a directory without the desktop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	workers := 4
	if value := os.Getenv("DISKLENS_SCAN_WORKERS"); value != "" {
		var err error
		workers, err = strconv.Atoi(value)
		if err != nil || workers < 1 || workers > 16 {
			t.Fatal("DISKLENS_SCAN_WORKERS must be 1 through 16")
		}
	}
	var s *Scan
	var err error
	if os.Getenv("DISKLENS_SCAN_BACKEND") == "mft" {
		s, err = walkNTFS(ctx, root, nil)
	} else {
		s, err = walkWindowsDirectory(ctx, root, nil, workers)
	}
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	h := sha256.New()
	var visit func(*Node)
	visit = func(n *Node) {
		rel, _ := filepath.Rel(s.Root.Path, n.Path)
		fmt.Fprintf(h, "%s\x00%d\x00%t\n", rel, n.Size, n.Dir)
		for _, child := range n.Children {
			visit(child)
		}
	}
	visit(s.Root)
	result := struct {
		Seconds                                                        float64
		Files, Folders, Errors, Skipped, DirectoryBatches, FileQueries int
		Bytes                                                          int64
		Digest                                                         string
	}{elapsed.Seconds(), s.Files, s.Folders, s.Errors, s.Skipped, s.DirectoryBatches, s.FileQueries, s.Root.Size, hex.EncodeToString(h.Sum(nil))}
	b, _ := json.MarshalIndent(result, "", "  ")
	t.Log(string(b))
	if output := os.Getenv("DISKLENS_SCAN_RESULT"); output != "" {
		if err := os.WriteFile(output, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScannerAllocationAndCancellation(t *testing.T) {
	root := t.TempDir()
	ordinary := filepath.Join(root, "ordinary.bin")
	compressed := filepath.Join(root, "compressed.bin")
	sparse := filepath.Join(root, "sparse.bin")
	if err := os.WriteFile(ordinary, bytes.Repeat([]byte{19}, 8193), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compressed, bytes.Repeat([]byte{31}, 1024*1024), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("compact.exe", "/C", "/I", compressed).CombinedOutput(); err != nil {
		t.Fatalf("compress: %v %s", err, out)
	}
	f, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if out, err := exec.Command("fsutil.exe", "sparse", "setflag", sparse).CombinedOutput(); err != nil {
		t.Fatalf("sparse: %v %s", err, out)
	}
	f, err = os.OpenFile(sparse, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1024 * 1024 * 1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Repeat([]byte{42}, 4096), 1024*1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if err := os.Link(ordinary, filepath.Join(root, "z-link.bin")); err != nil {
		t.Fatal(err)
	}
	var previous float64
	s, err := walkDirectory(context.Background(), root, func(p ScanProgress) {
		if p.Fraction < previous || p.Fraction > 1.000001 {
			t.Errorf("invalid progress %f after %f", p.Fraction, previous)
		}
		previous = p.Fraction
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Errors != 0 {
		t.Fatal(s.FirstError)
	}
	expected := int64(0)
	for _, n := range s.Root.Children {
		if n.Name == "z-link.bin" {
			if n.Size != 0 {
				t.Fatal("hardlink counted twice")
			}
			continue
		}
		var size int64
		if n.Name == "ordinary.bin" {
			size, _, err = diskSize(n.Path, nil)
		} else {
			size, err = compressedAllocation(n.Path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if n.Size != size {
			t.Fatalf("%s: scan %d, Windows allocation %d", n.Name, n.Size, size)
		}
		expected += size
	}
	if s.Root.Size != expected || previous != 1 {
		t.Fatalf("total/progress mismatch: %d/%d %f", s.Root.Size, expected, previous)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := walkDirectory(ctx, root, nil); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	t.Logf("Normal, compressed, sparse, hardlink allocation and cancellation verified; total %d bytes", expected)
}
