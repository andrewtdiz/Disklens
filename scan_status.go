package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func beginScan(root string) (*os.File, error) {
	path := snapshotPath(root) + ".scan.lock"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	locked, err := lockScanFile(file, true)
	if err != nil || !locked {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("scan status is in use for %s; try Refresh when the other scan finishes", root)
	}
	return file, nil
}

func scanInProgress(root string) (bool, error) {
	file, err := os.Open(snapshotPath(root) + ".scan.lock")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	locked, err := lockScanFile(file, false)
	return !locked, err
}
