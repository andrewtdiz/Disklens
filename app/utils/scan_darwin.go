package utils

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	bulkName       = 0x00000001
	bulkDevice     = 0x00000002
	bulkType       = 0x00000008
	bulkModified   = 0x00000400
	bulkFileID     = 0x02000000
	bulkError      = 0x20000000
	bulkReturned   = 0x80000000
	bulkLinks      = 0x00000001
	bulkAllocation = 0x00000004
)

type bulkAttributes struct {
	Count                                 uint16
	Reserved                              uint16
	Common, Volume, Directory, File, Fork uint32
}

type macIdentity struct {
	device uint32
	inode  uint64
}

type macEntry struct {
	name     string
	kind     uint32
	modified fileTimestamp
	size     int64
	links    uint32
	identity macIdentity
}

func readMacDirectory(ctx context.Context, fd int, path string, buffer []byte, s *Scan) ([]macEntry, error) {
	attrs := bulkAttributes{Count: 5, Common: bulkReturned | bulkError | bulkName | bulkDevice | bulkType | bulkModified | bulkFileID, File: bulkLinks | bulkAllocation}
	var entries []macEntry
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, _, errno := syscall.Syscall6(unix.SYS_GETATTRLISTBULK, uintptr(fd), uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 8, 0)
		s.DirectoryBatches++
		if errno != 0 {
			return entries, errno
		}
		if count == 0 {
			break
		}
		remaining := buffer
		for i := uintptr(0); i < count; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			record := remaining[:binary.LittleEndian.Uint32(remaining)]
			remaining = remaining[len(record):]
			common := binary.LittleEndian.Uint32(record[4:])
			file := binary.LittleEndian.Uint32(record[16:])
			entryError := syscall.Errno(binary.LittleEndian.Uint32(record[24:]))
			if common&bulkName == 0 {
				if entryError == 0 {
					entryError = syscall.EIO
				}
				s.RecordError(path, entryError)
				continue
			}
			nameStart := 28 + int(int32(binary.LittleEndian.Uint32(record[28:])))
			nameLength := int(binary.LittleEndian.Uint32(record[32:]))
			name := string(record[nameStart : nameStart+nameLength-1])
			if entryError != 0 {
				s.RecordError(filepath.Join(path, name), entryError)
				continue
			}
			kind := binary.LittleEndian.Uint32(record[40:])
			required := uint32(bulkName | bulkDevice | bulkType | bulkModified | bulkFileID)
			if common&required != required || (kind == 1 && file&(bulkLinks|bulkAllocation) != bulkLinks|bulkAllocation) {
				s.FileQueries++
				childPath := filepath.Join(path, name)
				info, err := os.Lstat(childPath)
				if err != nil {
					s.RecordError(childPath, err)
					continue
				}
				entries = append(entries, macEntryFromInfo(info))
				continue
			}
			entry := macEntry{name: name, kind: kind, modified: timestampFromTime(time.Unix(int64(binary.LittleEndian.Uint64(record[44:])), int64(binary.LittleEndian.Uint64(record[52:])))), identity: macIdentity{binary.LittleEndian.Uint32(record[36:]), binary.LittleEndian.Uint64(record[60:])}}
			if kind == 1 {
				entry.links = binary.LittleEndian.Uint32(record[68:])
				entry.size = int64(binary.LittleEndian.Uint64(record[72:]))
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func macEntryFromInfo(info os.FileInfo) macEntry {
	stat := info.Sys().(*syscall.Stat_t)
	kind := uint32(0)
	if info.IsDir() {
		kind = 2
	} else if info.Mode().IsRegular() {
		kind = 1
	}
	return macEntry{name: info.Name(), kind: kind, modified: timestampFromTime(info.ModTime()), size: stat.Blocks * 512, links: uint32(stat.Nlink), identity: macIdentity{uint32(stat.Dev), stat.Ino}}
}

type macDirectoryJob struct {
	node   *Node
	path   string
	weight float64
}

type macDirectoryResult struct {
	job      macDirectoryJob
	entries  []macEntry
	identity macIdentity
	scan     Scan
}

func readMacJob(ctx context.Context, job macDirectoryJob, buffer []byte) macDirectoryResult {
	result := macDirectoryResult{job: job}
	path := job.path
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		result.scan.RecordError(path, err)
		return result
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		result.scan.RecordError(path, err)
		return result
	}
	result.identity = macIdentity{uint32(stat.Dev), stat.Ino}
	result.scan.Folders = 1
	result.entries, err = readMacDirectory(ctx, fd, path, buffer, &result.scan)
	if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS) {
		result.scan.FallbackReason = "Some directories require individual metadata reads"
		result.entries = nil
		var items []os.DirEntry
		items, err = os.ReadDir(path)
		for _, item := range items {
			if ctx.Err() != nil {
				return result
			}
			result.scan.FileQueries++
			info, infoErr := item.Info()
			if infoErr != nil {
				result.scan.RecordError(filepath.Join(path, item.Name()), infoErr)
				continue
			}
			result.entries = append(result.entries, macEntryFromInfo(info))
		}
	}
	if err != nil {
		result.scan.RecordError(path, err)
	}
	return result
}

func walkMacDirectory(ctx context.Context, path string, progress func(ScanProgress)) (*Scan, error) {
	return walkMacDirectoryWorkers(ctx, path, progress, min(8, runtime.NumCPU()))
}

func walkMacDirectoryWorkers(ctx context.Context, path string, progress func(ScanProgress), workers int) (*Scan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || skipEntry(info) {
		return nil, errors.New("Choose a real directory, not a symbolic link")
	}
	s := &Scan{Backend: "macOS bulk", At: time.Now(), Root: &Node{Name: path, Dir: true, Modified: timestampFromTime(info.ModTime())}}
	report := scanReporter(s, progress)
	seen := map[macIdentity]*Node{}
	directories := map[macIdentity]bool{}
	workCtx, cancel := context.WithCancel(ctx)
	jobs := make(chan []macDirectoryJob)
	results := make(chan []macDirectoryResult, workers)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			buffer := make([]byte, 256*1024)
			for {
				select {
				case <-workCtx.Done():
					return
				case batch, ok := <-jobs:
					if !ok {
						return
					}
					completed := make([]macDirectoryResult, 0, len(batch))
					for _, job := range batch {
						if workCtx.Err() != nil {
							return
						}
						completed = append(completed, readMacJob(workCtx, job, buffer))
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
	pending := []macDirectoryJob{{s.Root, path, 1}}
	active := 0
	for len(pending) > 0 || active > 0 {
		var send chan []macDirectoryJob
		var next []macDirectoryJob
		if len(pending) > 0 {
			send = jobs
			next = append([]macDirectoryJob(nil), pending[max(0, len(pending)-16):]...)
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
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				s.Errors += result.scan.Errors
				s.Issues = append(s.Issues, result.scan.Issues...)
				if s.FirstError == "" {
					s.FirstError = result.scan.FirstError
				}
				s.DirectoryBatches += result.scan.DirectoryBatches
				s.FileQueries += result.scan.FileQueries
				if result.scan.FallbackReason != "" {
					s.FallbackReason = result.scan.FallbackReason
				}
				if result.scan.Folders != 0 && directories[result.identity] {
					s.Skipped++
					s.Fraction = min(1, s.Fraction+result.job.weight)
					continue
				}
				if result.scan.Folders != 0 {
					directories[result.identity] = true
					s.Folders++
				}
				directoryCount := 0
				for _, entry := range result.entries {
					if entry.kind == 2 {
						directoryCount++
					}
				}
				weight := result.job.weight / float64(directoryCount+1)
				s.Fraction = min(1, s.Fraction+weight)
				parent := result.job.node
				prefix := result.job.path
				if !strings.HasSuffix(prefix, "/") {
					prefix += "/"
				}
				children := make([]*Node, 0, len(result.entries))
				for _, entry := range result.entries {
					if entry.kind != 1 && entry.kind != 2 {
						s.Skipped++
						continue
					}
					child := &Node{Name: entry.name, Dir: entry.kind == 2, Modified: entry.modified, Parent: parent}
					children = append(children, child)
					if child.Dir {
						pending = append(pending, macDirectoryJob{child, prefix + entry.name, weight})
						continue
					}
					s.Files++
					child.Size = entry.size
					if entry.links > 1 {
						if previous := seen[entry.identity]; previous != nil {
							retainLinks(previous, child)
							if macPathBefore(prefix+entry.name, previous.FullPath()) {
								s.Bytes -= previous.Size
								previous.Size = 0
								seen[entry.identity] = child
							} else {
								child.Size = 0
							}
						} else {
							seen[entry.identity] = child
						}
					}
					s.Bytes += child.Size
				}
				parent.setChildren(children)
				report(result.job.path, false)
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

func macPathBefore(a, b string) bool {
	for {
		left, restA, moreA := strings.Cut(a, "/")
		right, restB, moreB := strings.Cut(b, "/")
		if left != right {
			return left < right
		}
		if !moreA || !moreB {
			return !moreA && moreB
		}
		a, b = restA, restB
	}
}
