//go:build e2e && !cli

package main

import (
	"context"
	"os"
	"testing"
	"time"
	"www.velocidex.com/golang/go-ntfs/parser"
)

func TestNTFSImageFilesystem(t *testing.T) {
	path := os.Getenv("DISKLENS_NTFS_IMAGE")
	if path == "" {
		t.Skip("Set DISKLENS_NTFS_IMAGE to a raw NTFS fixture image")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	s, err := scanNTFSImage(context.Background(), f, "fixture", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Errors != 0 {
		t.Fatal(s.FirstError)
	}
	t.Logf("Image scan: %s, %d files, %d folders, %d bytes", time.Since(start), s.Files, s.Folders, s.Root.Size)
	reader, err := parser.NewPagedReader(f, 4096, 256)
	if err != nil {
		t.Fatal(err)
	}
	ntfs, err := parser.GetNTFSContext(reader, 0, parser.GetDefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer ntfs.Close()
	root, err := ntfs.GetMFT(5)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var visit func(*Node, string)
	visit = func(n *Node, path string) {
		if path != "" {
			entry, err := root.Open(ntfs, path)
			if err != nil {
				t.Fatalf("Library lookup %s: %v", path, err)
			}
			if n.Dir != (entry.Flags().Value&2 != 0) {
				t.Fatalf("Directory mismatch %s", path)
			}
			if !n.Dir {
				var size int64
				for _, a := range entry.EnumerateAttributes(ntfs) {
					if a.Type().Value != 128 || a.Name() != "" {
						continue
					}
					if a.Resident().Value == 0 {
						size = (int64(a.Content_size()) + 7) &^ 7
					} else {
						if parser.IsCompressedOrSparse(a.Flags()) {
							for _, run := range a.RunList() {
								if run.RelativeUrnOffset != 0 {
									size += run.Length * ntfs.ClusterSize
								}
							}
						} else if a.Runlist_vcn_start() == 0 {
							size = int64(a.Allocated_size())
						}
					}
				}
				if n.Size != size {
					t.Fatalf("Allocation mismatch %s: %d vs parser %d", path, n.Size, size)
				}
				count++
			}
		}
		for _, c := range n.Children() {
			next := c.Name
			if path != "" {
				next = path + "/" + next
			}
			visit(c, next)
		}
	}
	visit(s.Root, "")
	for _, child := range s.Root.Children() {
		if !child.Dir || len(child.Children()) == 0 {
			continue
		}
		entry, err := root.Open(ntfs, child.Name)
		if err != nil {
			t.Fatal(err)
		}
		reference := uint64(entry.Record_number()) | uint64(entry.Sequence_value())<<48
		sub, err := scanNTFSImage(context.Background(), f, child.Name, reference, nil)
		if err != nil {
			t.Fatal(err)
		}
		if sub.Root.Size != child.Size || len(sub.Root.Children()) != len(child.Children()) {
			t.Fatal("Subtree selection differs from whole-image scan")
		}
		break
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanNTFSImage(ctx, f, "fixture", 0, nil); err != context.Canceled {
		t.Fatalf("NTFS cancellation: %v", err)
	}
	if count == 0 {
		t.Fatal("No files checked")
	}
	t.Logf("Verified %d file allocations and every path against the NTFS library", count)
}
