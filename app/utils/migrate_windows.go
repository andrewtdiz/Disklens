//go:build !cli

package utils

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func checkLegacyStatePath(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	buffer := make([]uint16, 32768)
	_, err = windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return err
	}
	resolved := strings.ToLower(windows.UTF16ToString(buffer))
	if strings.Contains(resolved, `\packages\`) && strings.Contains(resolved, `\localcache\`) {
		return fmt.Errorf("Windows redirected the old snapshot folder. Open Disklens from the Start menu once to import your desktop baseline")
	}
	return nil
}
