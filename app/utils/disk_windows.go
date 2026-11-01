//go:build !cli

package utils

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func skipEntry(info os.FileInfo) bool {
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&os.ModeSymlink != 0 || (ok && attrs.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0)
}

func longWindowsPath(path string) string {
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	return path
}

func diskSize(path string, info os.FileInfo) (int64, string, error) {
	path = longWindowsPath(path)
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, "", err
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return 0, "", err
	}
	defer windows.CloseHandle(h)
	var buffer struct {
		alignment uint64
		data      [24]byte
	}
	b := buffer.data[:]
	if err := windows.GetFileInformationByHandleEx(h, windows.FileStandardInfo, &b[0], uint32(len(b))); err != nil {
		return 0, "", fmt.Errorf("allocation: %w", err)
	}
	var id windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &id); err != nil {
		return 0, "", err
	}
	key := ""
	if id.NumberOfLinks > 1 {
		key = fmt.Sprintf("%x:%x:%x", id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow)
	}
	return int64(binary.LittleEndian.Uint64(b[:8])), key, nil
}
