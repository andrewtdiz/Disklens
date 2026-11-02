package utils

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ntQueryDirectoryFile = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryDirectoryFile")
var getDiskFreeSpace = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")
var getCompressedFileSize = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

type directoryRecord struct {
	node       *Node
	id         uint64
	attributes uint32
	err        error
}

type directoryBuffer struct {
	bytes                []byte
	queries, fileQueries int
	remote               bool
	clusterSize          int64
}

type directoryJob struct {
	node   *Node
	weight float64
}

type directoryResult struct {
	job                  directoryJob
	entries              []directoryRecord
	err                  error
	queries, fileQueries int
}

func WalkDirectory(ctx context.Context, path string, progress func(ScanProgress)) (*Scan, error) {
	s, err := walkNTFS(ctx, path, progress)
	if err == nil {
		return s, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s, fallbackErr := walkWindowsDirectory(ctx, path, progress, 4)
	if s != nil {
		s.FallbackReason = err.Error()
	}
	return s, fallbackErr
}

func walkWindowsDirectory(ctx context.Context, path string, progress func(ScanProgress), workers int) (*Scan, error) {
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
	s := &Scan{Backend: "Directory", At: time.Now(), Root: &Node{Name: path, Dir: true, Modified: timestampFromTime(info.ModTime())}}
	report := scanReporter(s, progress)
	volume, _ := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	remote := strings.HasPrefix(path, `\\`) || windows.GetDriveType(volume) == windows.DRIVE_REMOTE
	var geometry [4]uint32
	getDiskFreeSpace.Call(uintptr(unsafe.Pointer(volume)), uintptr(unsafe.Pointer(&geometry[0])), uintptr(unsafe.Pointer(&geometry[1])), uintptr(unsafe.Pointer(&geometry[2])), uintptr(unsafe.Pointer(&geometry[3])))
	clusterSize := int64(geometry[0]) * int64(geometry[1])
	bufferSize := 4 * 1024 * 1024
	if remote {
		bufferSize = 64 * 1024
	}
	workCtx, cancel := context.WithCancel(ctx)
	jobs := make(chan []directoryJob)
	results := make(chan []directoryResult, workers)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			buffer := &directoryBuffer{bytes: make([]byte, bufferSize), remote: remote, clusterSize: clusterSize}
			for {
				select {
				case <-workCtx.Done():
					return
				case batch, ok := <-jobs:
					if !ok {
						return
					}
					completed := make([]directoryResult, 0, len(batch))
					for _, job := range batch {
						if workCtx.Err() != nil {
							return
						}
						buffer.queries, buffer.fileQueries = 0, 0
						entries, err := readDirectoryRecords(workCtx, job.node, buffer)
						completed = append(completed, directoryResult{job, entries, err, buffer.queries, buffer.fileQueries})
					}
					select {
					case results <- completed:
					case <-workCtx.Done():
						return
					}
				}
			}
		})
	}
	defer func() { cancel(); group.Wait() }()
	pending := []directoryJob{{s.Root, 1}}
	active := 0
	seen := make(map[uint64]*Node)
	for len(pending) > 0 || active > 0 {
		var send chan []directoryJob
		var next []directoryJob
		if len(pending) > 0 {
			send = jobs
			next = append([]directoryJob(nil), pending[max(0, len(pending)-16):]...)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case send <- next:
			pending = pending[:len(pending)-len(next)]
			active += len(next)
		case completed := <-results:
			for _, result := range completed {
				active--
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if result.err != nil {
					if result.job.node == s.Root && (errors.Is(result.err, windows.ERROR_NOT_SUPPORTED) || errors.Is(result.err, windows.ERROR_INVALID_PARAMETER) || errors.Is(result.err, windows.ERROR_INVALID_FUNCTION)) {
						cancel()
						group.Wait()
						return walkDirectoryPortable(ctx, path, progress)
					}
					s.RecordError(result.job.node.FullPath(), result.err)
				}
				s.Folders++
				s.DirectoryBatches += result.queries
				s.FileQueries += result.fileQueries
				directories := 0
				for _, entry := range result.entries {
					if entry.node.Dir && entry.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
						directories++
					}
				}
				weight := result.job.weight / float64(directories+1)
				s.Fraction += weight
				parent := result.job.node
				for _, entry := range result.entries {
					n := entry.node
					if entry.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
						s.Skipped++
						continue
					}
					parent.addChild(n)
					if entry.err != nil {
						s.RecordError(n.FullPath(), entry.err)
					}
					if n.Dir {
						pending = append(pending, directoryJob{n, weight})
						continue
					}
					s.Files++
					if entry.id != 0 {
						if previous := seen[entry.id]; previous != nil {
							retainLinks(previous, n)
							if pathBefore(n.FullPath(), previous.FullPath()) {
								s.Bytes -= previous.Size
								previous.Size = 0
								seen[entry.id] = n
							} else {
								n.Size = 0
							}
						} else {
							seen[entry.id] = n
						}
					}
					s.Bytes += n.Size
				}
				report(parent.FullPath(), false)
			}
		}
	}
	close(jobs)
	group.Wait()
	var finalize func(*Node) error
	finalize = func(n *Node) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, child := range n.Children() {
			if child.Dir {
				if err := finalize(child); err != nil {
					return err
				}
			}
			n.Size += child.Size
		}
		sortNodes(n.Children())
		return nil
	}
	if err := finalize(s.Root); err != nil {
		return nil, err
	}
	s.Fraction = 1
	report(path, true)
	return s, nil
}

func pathBefore(a, b string) bool {
	left, right := strings.Split(a, `\`), strings.Split(b, `\`)
	for i := 0; i < min(len(left), len(right)); i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) < len(right)
}

func readDirectoryRecords(ctx context.Context, parent *Node, buffer *directoryBuffer) ([]directoryRecord, error) {
	path, err := windows.UTF16PtrFromString(longWindowsPath(parent.FullPath()))
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
		var status windows.IO_STATUS_BLOCK
		code, _, _ := ntQueryDirectoryFile.Call(uintptr(h), 0, 0, 0, uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&buffer.bytes[0])), uintptr(len(buffer.bytes)), 38, 0, 0, 0)
		if uint32(code) == uint32(windows.STATUS_NO_MORE_FILES) {
			return entries, nil
		}
		if code != 0 {
			return entries, windows.NTStatus(code).Errno()
		}
		if status.Information == 0 {
			return entries, windows.ERROR_INVALID_DATA
		}
		for offset := 0; ; {
			if err := ctx.Err(); err != nil {
				return entries, err
			}
			b := buffer.bytes[offset:]
			length := int(binary.LittleEndian.Uint32(b[60:64])) / 2
			for i := 0; i < length; i++ {
				name[i] = binary.LittleEndian.Uint16(b[80+i*2 : 82+i*2])
			}
			filename := string(utf16.Decode(name[:length]))
			if filename != "." && filename != ".." {
				attributes := binary.LittleEndian.Uint32(b[56:60])
				ticks := binary.LittleEndian.Uint64(b[24:32])
				n := &Node{Name: filename, Parent: parent, Dir: attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0, Modified: timestampFromNTFS(ticks)}
				entry := directoryRecord{node: n, id: binary.LittleEndian.Uint64(b[72:80]), attributes: attributes}
				if !n.Dir && attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
					n.Size = int64(binary.LittleEndian.Uint64(b[48:56]))
					logicalSize := int64(binary.LittleEndian.Uint64(b[40:48]))
					if n.Size == 0 && !buffer.remote && (attributes&(windows.FILE_ATTRIBUTE_COMPRESSED|windows.FILE_ATTRIBUTE_SPARSE_FILE) != 0 || (buffer.clusterSize > 0 && logicalSize > buffer.clusterSize)) {
						buffer.fileQueries++
						n.Size, entry.err = compressedAllocation(n.FullPath())
					}
				}
				entries = append(entries, entry)
			}
			next := int(binary.LittleEndian.Uint32(b[:4]))
			if next == 0 {
				break
			}
			offset += next
		}
	}
}

func compressedAllocation(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(longWindowsPath(path))
	if err != nil {
		return 0, err
	}
	var high uint32
	low, _, err := getCompressedFileSize.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == 0xffffffff && err != windows.ERROR_SUCCESS {
		return 0, err
	}
	return int64(uint64(high)<<32 | uint64(uint32(low))), nil
}

func walkAffectedDirectory(ctx context.Context, path string) (*Scan, error) {
	return walkWindowsDirectory(ctx, path, nil, 4)
}
