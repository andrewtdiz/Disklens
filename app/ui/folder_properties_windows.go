//go:build !cli

package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type shellExecuteInfo struct {
	size, mask                        uint32
	window                            uintptr
	verb, file, parameters, directory *uint16
	show                              int32
	instance, idList                  uintptr
	class                             *uint16
	classKey                          uintptr
	hotKey                            uint32
	icon, process                     uintptr
}

func showFolderProperties(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("properties")
	info := shellExecuteInfo{mask: 0x0000000c | 0x00000400, verb: verb, file: file, show: windows.SW_SHOWNORMAL}
	info.size = uint32(unsafe.Sizeof(info))
	result, _, err := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW").Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return err
	}
	return nil
}
