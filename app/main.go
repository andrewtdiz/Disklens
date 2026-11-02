//go:build !cli

package main

import (
	"disklens/app/ui"
	"os"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "diff" || os.Args[1] == "scan" || os.Args[1] == "baseline" || os.Args[1] == "coverage" || os.Args[1] == "--help" || os.Args[1] == "-h") {
		os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
	}
	ui.Run()
}
