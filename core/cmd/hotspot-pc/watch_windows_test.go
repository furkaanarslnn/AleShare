package main

import (
	"testing"
	"time"
)

func TestReconcileRunsOneAtATime(t *testing.T) {
	reconcileMutex = `Local\HotspotReconcileTest`
	unlock := lockReconcile()
	got := make(chan time.Time)
	go func() {
		u := lockReconcile()
		got <- time.Now()
		u()
	}()
	time.Sleep(300 * time.Millisecond)
	released := time.Now()
	unlock()
	select {
	case at := <-got:
		if at.Before(released) {
			t.Fatal("the second run did not wait for the first")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second run never started")
	}
}

func TestWlanStatesIsCheapAndSteady(t *testing.T) {
	start := time.Now()
	a, b := wlanStates(), wlanStates()
	if a != b {
		t.Fatalf("nothing changed, yet %q became %q", a, b)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("two looks took %v", d)
	}
}

// While a setup holds its mark, a look at it finds it; afterwards it is gone.
func TestSetupMark(t *testing.T) {
	setupMutex = `Local\HotspotSetupTest`
	if setupRunning() {
		t.Fatal("a setup is said to run before any began")
	}
	resume := holdSetup()
	if !setupRunning() {
		t.Fatal("the setup's mark is not seen")
	}
	resume()
	resume() // a second call does nothing
	if setupRunning() {
		t.Fatal("the mark outlived the setup")
	}
}
