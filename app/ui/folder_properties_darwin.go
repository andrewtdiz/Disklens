//go:build !cli

package ui

import "os/exec"

func showFolderProperties(path string) error {
	script := "on run argv\n tell application \"Finder\"\n activate\n open information window of (POSIX file (item 1 of argv) as alias)\n end tell\nend run"
	return exec.Command("osascript", "-e", script, "--", path).Run()
}
