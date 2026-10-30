package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

func walkDirectory(ctx context.Context, path string, progress func(ScanProgress)) (*Scan, error) {
	return walkDirectoryPortable(ctx, path, progress)
}

func skipEntry(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }

func diskSize(path string, info os.FileInfo) (int64, string, error) {
	stat := info.Sys().(*syscall.Stat_t)
	key := ""
	if stat.Nlink > 1 {
		key = fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	}
	return stat.Blocks * 512, key, nil
}
