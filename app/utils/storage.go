package utils

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DataDirectory() string {
	if dir := os.Getenv("DISKLENS_DATA"); dir != "" {
		return dir
	}
	if runtime.GOOS == "windows" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".disklens")
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Disklens")
	}
	return filepath.Join(dir, "Disklens")
}

func SnapshotPath(root string) string {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return filepath.Join(DataDirectory(), "snapshots", fmt.Sprintf("%x.json", sha256.Sum256([]byte(root))))
}

func SaveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".disklens-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(b); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
