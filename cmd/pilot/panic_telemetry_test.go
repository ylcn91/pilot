package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/logging"
)

func TestInstallPanicHookIncrementsCounter(t *testing.T) {
	// Reset shared state so the test is order-independent.
	panicMetrics = &panicCounters{counts: make(map[string]uint64)}
	registerPanicServer(nil)
	t.Cleanup(func() { logging.SetPanicHook(nil) })

	installPanicHook()

	before := panicMetrics.PanicCount("test.component")

	// RunSafely recovers the panic synchronously and invokes the hook.
	logging.RunSafely("test.component", func() {
		panic("boom")
	})

	after := panicMetrics.PanicCount("test.component")
	if after != before+1 {
		t.Errorf("PanicCount = %d, want %d", after, before+1)
	}
	if total := panicMetrics.PanicTotal(); total != 1 {
		t.Errorf("PanicTotal = %d, want 1", total)
	}
}

func TestPanicHookCountsPerComponent(t *testing.T) {
	panicMetrics = &panicCounters{counts: make(map[string]uint64)}
	registerPanicServer(nil)
	t.Cleanup(func() { logging.SetPanicHook(nil) })

	installPanicHook()

	logging.RunSafely("a", func() { panic("x") })
	logging.RunSafely("a", func() { panic("y") })
	logging.RunSafely("b", func() { panic("z") })

	if got := panicMetrics.PanicCount("a"); got != 2 {
		t.Errorf("component a = %d, want 2", got)
	}
	if got := panicMetrics.PanicCount("b"); got != 1 {
		t.Errorf("component b = %d, want 1", got)
	}
	if got := panicMetrics.PanicTotal(); got != 3 {
		t.Errorf("total = %d, want 3", got)
	}
}

func TestRegisterPanicServerNilSafe(t *testing.T) {
	panicMetrics = &panicCounters{counts: make(map[string]uint64)}
	registerPanicServer(nil)
	t.Cleanup(func() { logging.SetPanicHook(nil) })

	installPanicHook()

	// With no server registered, the hook must not panic when forwarding.
	logging.RunSafely("comp", func() { panic("no server") })

	if got := panicMetrics.PanicCount("comp"); got != 1 {
		t.Errorf("count = %d, want 1", got)
	}
}
