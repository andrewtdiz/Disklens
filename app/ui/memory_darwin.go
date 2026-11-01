//go:build !cli

package ui

type idleMemory struct{}

func (*idleMemory) observe(bool, bool) {}
func (*idleMemory) requestTrim()       {}
func (*idleMemory) start() func()      { return func() {} }
func trimWorkingSet()                  {}
