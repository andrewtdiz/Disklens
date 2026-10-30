package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

type directoryRecord struct {
	node       *Node
	id         uint64
	attributes uint32
}

type directoryBuffer struct {
	alignment uint64
	bytes     [64 * 1024]byte
	queries   int
}

func walkDirectory(ctx context.Context, path string, progress func(ScanProgress)) (*Scan, error) {
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || skipEntry(info) {
		return nil, errors.New("Choose a real directory, not a symbolic link or reparse point")
	}
	s := &Scan{At: time.Now(), Root: &Node{Name: info.Name(), Path: path, Dir: true, Modified: info.ModTime()}}
	report := scanReporter(s, progress)
	buffer := new(directoryBuffer)
	seen := make(map[uint64]struct{})
	var visit func(*Node, float64, float64) error
	visit = func(parent *Node, start, span float64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.Folders++
		report(parent.Path, false)
		entries, err := readDirectoryRecords(ctx, parent, buffer)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if parent == s.Root && (errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_INVALID_FUNCTION)) {
				return err
			}
			s.recordError(parent.Path, err)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].node.Name < entries[j].node.Name })
		for i, entry := range entries {
			s.Fraction = start + span*float64(i)/float64(len(entries))
			if err := ctx.Err(); err != nil {
				return err
			}
			n := entry.node
			if entry.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				s.Skipped++
				continue
			}
			parent.Children = append(parent.Children, n)
			if n.Dir {
				if err := visit(n, s.Fraction, span/float64(len(entries))); err != nil {
					return err
				}
			} else {
				s.Files++
				if entry.attributes&(windows.FILE_ATTRIBUTE_COMPRESSED|windows.FILE_ATTRIBUTE_SPARSE_FILE) != 0 {
					s.FileQueries++
					size, _, err := diskSize(n.Path, nil)
					if err != nil {
						s.recordError(n.Path, err)
						n.Size = 0
					} else {
						n.Size = size
					}
				}
				if entry.id != 0 {
					if _, duplicate := seen[entry.id]; duplicate {
						n.Size = 0
					} else {
						seen[entry.id] = struct{}{}
					}
				}
				s.Bytes += n.Size
				if s.Files%256 == 0 {
					report(parent.Path, false)
				}
			}
			parent.Size += n.Size
		}
		sortNodes(parent.Children)
		return nil
	}
	if err := visit(s.Root, 0, 1); err != nil {
		if errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
			return walkDirectoryPortable(ctx, path, progress)
		}
		return nil, err
	}
	s.Fraction = 1
	s.DirectoryBatches = buffer.queries
	report(path, true)
	return s, nil
}

func readDirectoryRecords(ctx context.Context, parent *Node, buffer *directoryBuffer) ([]directoryRecord, error) {
	path, err := windows.UTF16PtrFromString(longWindowsPath(parent.Path))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.FILE_LIST_DIRECTORY, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	var entries []directoryRecord
	var name [256]uint16
	for {
		if err := ctx.Err(); err != nil {
			return entries, err
		}
		buffer.queries++
		err := windows.GetFileInformationByHandleEx(h, windows.FileIdBothDirectoryInfo, &buffer.bytes[0], uint32(len(buffer.bytes)))
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return entries, nil
		}
		if err != nil {
			return entries, err
		}
		for offset := 0; ; {
			if err := ctx.Err(); err != nil {
				return entries, err
			}
			b := buffer.bytes[offset:]
			length := int(binary.LittleEndian.Uint32(b[60:64])) / 2
			for i := 0; i < length; i++ {
				name[i] = binary.LittleEndian.Uint16(b[104+i*2 : 106+i*2])
			}
			filename := string(utf16.Decode(name[:length]))
			if filename != "." && filename != ".." {
				attributes := binary.LittleEndian.Uint32(b[56:60])
				ticks := binary.LittleEndian.Uint64(b[24:32])
				n := &Node{Name: filename, Path: filepath.Join(parent.Path, filename), Parent: parent, Dir: attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0, Modified: time.Unix(int64(ticks/10000000)-11644473600, int64(ticks%10000000)*100).Local()}
				if !n.Dir {
					n.Size = int64(binary.LittleEndian.Uint64(b[48:56]))
				}
				entries = append(entries, directoryRecord{node: n, id: binary.LittleEndian.Uint64(b[96:104]), attributes: attributes})
			}
			next := int(binary.LittleEndian.Uint32(b[:4]))
			if next == 0 {
				break
			}
			offset += next
		}
	}
}
