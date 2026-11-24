package wgcore

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"time"

	"hotspot/core/internal/gateway"
)

// The functions in this file are measuring aids. The app exposes them only in
// debug builds, through a broadcast receiver that release builds do not contain.

// SetTuning overrides the runtime settings used while the gateway runs: the
// number of OS threads running Go code and the garbage collector's GOGC. Zero
// keeps the current value. When the gateway runs, the change applies at once.
// It returns the effective settings.
func (c *Core) SetTuning(procs, gcPercent int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if procs > 0 {
		c.procs = procs
	}
	if gcPercent != 0 {
		c.gcPercent = gcPercent
	}
	if c.untune != nil {
		runtime.GOMAXPROCS(min(c.procs, runtime.NumCPU()))
		debug.SetGCPercent(c.gcPercent)
	}
	return fmt.Sprintf("procs=%d gc=%d running=%v", runtime.GOMAXPROCS(0), c.gcPercent, c.untune != nil)
}

// SetCoalesce sets, in microseconds, how long a flowing stream waits to fill a
// packet batch (0 turns the wait off). It returns the effective value.
func (c *Core) SetCoalesce(micros int) string {
	if micros >= 0 {
		gateway.SetCoalesceWindow(time.Duration(micros) * time.Microsecond)
	}
	return fmt.Sprintf("coalesce=%dus", micros)
}

// StartCPUProfile writes a CPU profile to path until StopCPUProfile. The file is
// in Go's pprof format: `go tool pprof -top file` reads it on a PC.
func (c *Core) StartCPUProfile(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.profile != nil {
		return errors.New("wgcore: already profiling")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return err
	}
	c.profile = f
	return nil
}

// StopCPUProfile ends the profile started by StartCPUProfile.
func (c *Core) StopCPUProfile() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.profile == nil {
		return
	}
	pprof.StopCPUProfile()
	c.profile.Close()
	c.profile = nil
}
