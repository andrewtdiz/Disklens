//go:build !cli

package main

import (
	"runtime/metrics"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

type idleMemory struct {
	mu      sync.Mutex
	enabled bool
	busy    bool
	pending bool
	changed time.Time
}

func (m *idleMemory) observe(enabled, busy bool) {
	m.mu.Lock()
	m.enabled, m.busy = enabled, busy
	m.mu.Unlock()
}

func (m *idleMemory) requestTrim() {
	m.mu.Lock()
	m.pending = true
	m.changed = time.Now()
	m.mu.Unlock()
}

func (m *idleMemory) start() func() {
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		samples := []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}
		metrics.Read(samples)
		previous := samples[0].Value.Uint64()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				metrics.Read(samples)
				cycles := samples[0].Value.Uint64()
				m.mu.Lock()
				if cycles != previous {
					previous = cycles
					m.pending = true
					m.changed = now
				}
				if m.enabled && !m.busy && m.pending && now.Sub(m.changed) >= time.Second {
					trimWorkingSet()
					m.pending = false
				}
				m.mu.Unlock()
			}
		}
	}()
	return func() { close(stop); <-finished }
}

func trimWorkingSet() {
	_ = windows.SetProcessWorkingSetSizeEx(windows.CurrentProcess(), ^uintptr(0), ^uintptr(0), 0)
}
