//go:build !cli

package utils

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func walkNTFS(ctx context.Context, path string, progress func(ScanProgress)) (*Scan, error) {
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || skipEntry(info) {
		return nil, errors.New("Fast scan requires a real directory")
	}
	p, err := windows.UTF16PtrFromString(longWindowsPath(path))
	if err != nil {
		return nil, err
	}
	var mount, volume, filesystem [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumePathName(p, &mount[0], uint32(len(mount))); err != nil {
		return nil, err
	}
	if err := windows.GetVolumeInformation(&mount[0], nil, 0, nil, nil, nil, &filesystem[0], uint32(len(filesystem))); err != nil {
		return nil, err
	}
	if windows.UTF16ToString(filesystem[:]) != "NTFS" {
		return nil, errors.New("Fast scan requires NTFS")
	}
	if err := windows.GetVolumeNameForVolumeMountPoint(&mount[0], &volume[0], uint32(len(volume))); err != nil {
		return nil, err
	}
	directory, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var id windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(directory, &id)
	windows.CloseHandle(directory)
	if err != nil {
		return nil, err
	}
	volumePath := strings.TrimSuffix(windows.UTF16ToString(volume[:]), `\`)
	v, _ := windows.UTF16PtrFromString(volumePath)
	handle, err := windows.CreateFile(v, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_NO_BUFFERING, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(handle)
	storage := make([]byte, 4*1024*1024+4095)
	alignment := (-uintptr(unsafe.Pointer(unsafe.SliceData(storage)))) & 4095
	disk := &rawVolumeReader{handle: handle, buffer: storage[alignment : alignment+4*1024*1024]}
	return scanNTFSImage(ctx, disk, path, uint64(id.FileIndexHigh)<<32|uint64(id.FileIndexLow), progress)
}

type rawVolumeReader struct {
	handle windows.Handle
	buffer []byte
}

func (r *rawVolumeReader) ReadAt(dst []byte, offset int64) (int, error) {
	total := 0
	for len(dst) > 0 {
		aligned := offset &^ 4095
		prefix := int(offset - aligned)
		count := min(len(dst), len(r.buffer)-prefix)
		length := (prefix + count + 4095) &^ 4095
		if _, err := windows.Seek(r.handle, aligned, 0); err != nil {
			return total, err
		}
		var read uint32
		if err := windows.ReadFile(r.handle, r.buffer[:length], &read, nil); err != nil {
			return total, err
		}
		if int(read) <= prefix {
			return total, io.EOF
		}
		copied := copy(dst[:count], r.buffer[prefix:min(int(read), prefix+count)])
		total += copied
		offset += int64(copied)
		dst = dst[copied:]
		if copied < count {
			return total, io.EOF
		}
	}
	return total, nil
}
