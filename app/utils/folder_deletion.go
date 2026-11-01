//go:build !cli

package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type FolderDeletion struct {
	Root     *os.Root
	relative string
	Path     string
	info     os.FileInfo
	Node     *Node
}

func PrepareFolderDeletion(rootPath, target string) (*FolderDeletion, error) {
	rootPath, err := filepath.Abs(filepath.Clean(rootPath))
	if err != nil {
		return nil, err
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootPath, target)
	if err != nil {
		return nil, err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("Choose a folder inside the scanned directory")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat(relative)
	if err != nil {
		root.Close()
		return nil, err
	}
	if !info.IsDir() || skipEntry(info) {
		root.Close()
		return nil, errors.New("The selected path is no longer a regular folder")
	}
	return &FolderDeletion{Root: root, relative: relative, Path: target, info: info}, nil
}
