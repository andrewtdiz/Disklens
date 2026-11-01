//go:build !cli && !e2e

package ui

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func prepareDesktop() (bool, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return true, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	args := make([]string, 0, len(os.Args)-1)
	for _, arg := range os.Args[1:] {
		args = append(args, syscall.EscapeArg(arg))
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return false, err
	}
	parameters, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return false, err
	}
	directory, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		return false, err
	}
	return false, windows.ShellExecute(0, verb, file, parameters, directory, windows.SW_SHOWNORMAL)
}
