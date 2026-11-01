//go:build !cli

package main

import "golang.org/x/sys/windows"

func trimWorkingSet() {
	_ = windows.SetProcessWorkingSetSizeEx(windows.CurrentProcess(), ^uintptr(0), ^uintptr(0), 0)
}
