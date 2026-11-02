package utils

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func walkDirectoryPortable(ctx context.Context, path string, progress func(ScanProgress)) (*Scan, error) {
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
	s := &Scan{Backend: "Directory", At: time.Now()}
	report := scanReporter(s, progress)
	seen := map[string]*Node{}
	var walk func(string, os.FileInfo, *Node, float64, float64) (*Node, error)
	walk = func(path string, info os.FileInfo, parent *Node, start, span float64) (*Node, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := &Node{Name: info.Name(), Dir: info.IsDir(), Modified: timestampFromTime(info.ModTime()), Parent: parent}
		if parent == nil {
			n.Name = path
		}
		if !n.Dir {
			s.Files++
			s.FileQueries++
			size, identity, err := diskSize(path, info)
			if err != nil {
				s.RecordError(path, err)
				return n, nil
			}
			if identity == "" || seen[identity] == nil {
				n.Size = size
				if identity != "" {
					seen[identity] = n
				}
			} else {
				retainLinks(seen[identity], n)
			}
			s.Bytes += n.Size
			if s.Files%256 == 0 {
				report(filepath.Dir(path), false)
			}
			return n, nil
		}
		s.Folders++
		report(path, false)
		s.DirectoryBatches++
		entries, err := os.ReadDir(path)
		if err != nil {
			s.RecordError(path, err)
			return n, nil
		}
		for i, entry := range entries {
			s.Fraction = start + span*float64(i)/float64(len(entries))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			childPath := filepath.Join(path, entry.Name())
			childInfo, err := entry.Info()
			if err != nil {
				s.RecordError(childPath, err)
				continue
			}
			if skipEntry(childInfo) || (!childInfo.IsDir() && !childInfo.Mode().IsRegular()) {
				s.Skipped++
				continue
			}
			child, err := walk(childPath, childInfo, n, s.Fraction, span/float64(len(entries)))
			if err != nil {
				return nil, err
			}
			n.addChild(child)
			n.Size += child.Size
		}
		sortNodes(n.Children())
		return n, nil
	}
	s.Root, err = walk(path, info, nil, 0, 1)
	if err == nil {
		s.Fraction = 1
	}
	report(path, true)
	return s, err
}
