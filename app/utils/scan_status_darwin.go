package utils

import (
	"errors"
	"os"
	"syscall"
)

func lockScanFile(file *os.File, exclusive bool) (bool, error) {
	flags := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		flags = syscall.LOCK_EX | syscall.LOCK_NB
	}
	err := syscall.Flock(int(file.Fd()), flags)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}
