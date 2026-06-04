package main

import (
	"sync"
	"sync/atomic"

	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/logging"
)

// panicCounters tracks pilot_panics_total{component}: the number of goroutine
// panics recovered by logging.SafeGo/RunSafely, keyed by component. The gateway
// metrics exposition lives in internal/* (which cmd/pilot does not own), so the
// counter is kept here and surfaced via PanicCount/PanicTotal for tests and any
// future wiring into the prometheus writer.
type panicCounters struct {
	mu     sync.Mutex
	counts map[string]uint64
}

var panicMetrics = &panicCounters{counts: make(map[string]uint64)}

func (p *panicCounters) inc(component string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[component]++
}

// PanicCount returns the recovered-panic count for a component.
func (p *panicCounters) PanicCount(component string) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[component]
}

// PanicTotal returns the total recovered-panic count across all components.
func (p *panicCounters) PanicTotal() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var total uint64
	for _, c := range p.counts {
		total += c
	}
	return total
}

// liveServer holds the active gateway server so the process-wide panic hook can
// forward RecordPanic without the closure capturing a server that doesn't exist
// at install time.
var liveServer atomic.Pointer[gateway.Server]

// panicRecorder receives recovered-panic events keyed by component (#31). The
// autopilot Metrics satisfies it, surfacing pilot_panics_total on /metrics.
type panicRecorder interface {
	RecordPanic(component string)
}

// livePanicRecorder holds the active panic recorder. A pointer-to-interface is
// used so the atomic stores a stable, comparable value.
var livePanicRecorder atomic.Pointer[panicRecorder]

// installPanicHook wires the process-wide panic hook once. On every recovered
// goroutine panic it increments the cmd-local pilot_panics_total{component},
// forwards the event to the gateway server's liveness tracker, and records it on
// the autopilot metrics recorder so the counter shows on the Prometheus endpoint.
func installPanicHook() {
	logging.SetPanicHook(func(component string, _ any) {
		panicMetrics.inc(component)
		if srv := liveServer.Load(); srv != nil {
			srv.RecordPanic()
		}
		if rec := livePanicRecorder.Load(); rec != nil {
			(*rec).RecordPanic(component)
		}
	})
}

// registerPanicServer makes the given gateway server the target for RecordPanic
// forwarding from the panic hook. Safe to call with nil (clears the target).
func registerPanicServer(srv *gateway.Server) {
	liveServer.Store(srv)
}

// registerPanicRecorder makes the given recorder the target for component-keyed
// panic counting from the panic hook. Safe to call with nil (clears the target).
func registerPanicRecorder(rec panicRecorder) {
	if rec == nil {
		livePanicRecorder.Store(nil)
		return
	}
	livePanicRecorder.Store(&rec)
}
