//go:build windows && e2e && !cli

package main

import (
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"sort"
	"testing"
	"time"
	"unsafe"
)

func TestScannerBackendParity(t *testing.T) {
	root := os.Getenv("DISKLENS_PARITY_ROOT")
	if root == "" {
		t.Skip("Set DISKLENS_PARITY_ROOT for elevated backend parity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	normal, err := walkWindowsDirectory(ctx, root, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Directory %.2fs: %d files, %d folders, %d bytes, %d errors", time.Since(start).Seconds(), normal.Files, normal.Folders, normal.Bytes, normal.Errors)
	start = time.Now()
	fast, err := walkNTFS(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MFT %.2fs: %d files, %d folders, %d bytes, %d errors", time.Since(start).Seconds(), fast.Files, fast.Folders, fast.Bytes, fast.Errors)
	type difference struct {
		path         string
		normal, fast int64
	}
	var differences []difference
	missing, added := 0, 0
	var compare func(*Node, *Node)
	compare = func(a, b *Node) {
		byName := make(map[string]*Node, len(a.Children()))
		for _, c := range a.Children() {
			byName[c.Name] = c
		}
		for _, c := range b.Children() {
			prior := byName[c.Name]
			delete(byName, c.Name)
			if prior == nil {
				added++
				continue
			}
			if prior.Dir && c.Dir {
				compare(prior, c)
			} else if prior.Size != c.Size {
				differences = append(differences, difference{c.FullPath(), prior.Size, c.Size})
			}
		}
		missing += len(byName)
	}
	compare(normal.Root, fast.Root)
	sort.Slice(differences, func(i, j int) bool {
		a, b := differences[i], differences[j]
		da, db := a.fast-a.normal, b.fast-b.normal
		if da < 0 {
			da = -da
		}
		if db < 0 {
			db = -db
		}
		return da > db
	})
	t.Logf("different allocations %d; missing roots %d; added roots %d", len(differences), missing, added)
	for _, d := range differences[:min(20, len(differences))] {
		physical, _, err := diskSize(d.path, nil)
		compressed, ce := compressedAllocation(d.path)
		t.Logf("%s: directory=%d mft=%d file-allocation=%d (%v) compressed=%d (%v)", d.path, d.normal, d.fast, physical, err, compressed, ce)
	}
	if os.Getenv("DISKLENS_REQUIRE_PARITY") == "1" && (len(differences) != 0 || missing != 0 || added != 0) {
		t.Fatal(fmt.Sprintf("Backends disagree on fixed fixture: %d differences", len(differences)))
	}
}

func TestNTFSUnbufferedImage(t *testing.T) {
	path := os.Getenv("DISKLENS_NTFS_IMAGE")
	if path == "" {
		t.Skip("Set DISKLENS_NTFS_IMAGE to a raw NTFS fixture image")
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_NO_BUFFERING, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	storage := make([]byte, 4*1024*1024+4095)
	alignment := (-uintptr(unsafe.Pointer(unsafe.SliceData(storage)))) & 4095
	reader := &rawVolumeReader{handle: h, buffer: storage[alignment : alignment+4*1024*1024]}
	s, err := scanNTFSImage(context.Background(), reader, "fixture", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Root.Size != 2757984 || s.Files != 21 || s.Folders != 11 {
		t.Fatalf("Unbuffered image mismatch: %d bytes, %d files, %d folders", s.Root.Size, s.Files, s.Folders)
	}
	t.Log("Aligned unbuffered Windows I/O produced identical NTFS tree and allocations")
}
