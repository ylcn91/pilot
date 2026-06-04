package logging

import (
	"sync"
	"testing"
)

func TestRunSafely_RecoversAndHooks(t *testing.T) {
	prev := panicHook
	t.Cleanup(func() { panicHook = prev })

	var (
		mu        sync.Mutex
		gotComp   string
		gotPanic  any
		hookCalls int
	)
	SetPanicHook(func(component string, recovered any) {
		mu.Lock()
		defer mu.Unlock()
		gotComp = component
		gotPanic = recovered
		hookCalls++
	})

	// Must not propagate the panic.
	RunSafely("worker", func() { panic("boom") })

	mu.Lock()
	defer mu.Unlock()
	if hookCalls != 1 {
		t.Fatalf("hookCalls = %d, want 1", hookCalls)
	}
	if gotComp != "worker" {
		t.Errorf("component = %q, want %q", gotComp, "worker")
	}
	if gotPanic != "boom" {
		t.Errorf("panic = %v, want %q", gotPanic, "boom")
	}
}

func TestRunSafely_NoPanicNoHook(t *testing.T) {
	prev := panicHook
	t.Cleanup(func() { panicHook = prev })

	called := false
	SetPanicHook(func(string, any) { called = true })

	ran := false
	RunSafely("worker", func() { ran = true })

	if !ran {
		t.Error("fn was not run")
	}
	if called {
		t.Error("panic hook fired on a clean run")
	}
}

func TestSafeGo_RecoversInGoroutine(t *testing.T) {
	prev := panicHook
	t.Cleanup(func() { panicHook = prev })

	done := make(chan struct{})
	SetPanicHook(func(string, any) { close(done) })

	SafeGo("async", func() { panic("async boom") })

	<-done // blocks forever (test fails on timeout) if the panic escaped
}
