//go:build !cli

package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

func MigrateState() error {
	if runtime.GOOS != "windows" || os.Getenv("DISKLENS_DATA") != "" {
		return nil
	}
	marker := filepath.Join(DataDirectory(), "migration.json")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	previous, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	previous = filepath.Join(previous, "Disklens")
	if err := checkLegacyStatePath(previous); err != nil {
		return err
	}
	if err := copyLegacyState(previous, DataDirectory()); err != nil {
		return err
	}
	return SaveJSON(marker, map[string]string{"imported_from": previous})
}

func copyLegacyState(previous, destination string) error {
	paths, err := filepath.Glob(filepath.Join(previous, "snapshots", "*.json"))
	if err != nil {
		return err
	}
	paths = append(paths, filepath.Join(previous, "settings.json"))
	for _, source := range paths {
		rel, err := filepath.Rel(previous, source)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		contents, err := os.ReadFile(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := SaveJSON(target, json.RawMessage(contents)); err != nil {
			return err
		}
	}
	return nil
}
