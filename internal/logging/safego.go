package logging

import "runtime/debug"

// panicHook, if set, is invoked after a recovered goroutine panic with the
// component label and the recovered value. cmd/pilot wires it to the panic
// counter and the gateway liveness tracker; logging itself stays
// dependency-free so it can be imported from anywhere without an import cycle.
var panicHook func(component string, recovered any)

// SetPanicHook installs a process-wide hook invoked on every panic recovered
// by SafeGo / RunSafely / Recover. Pass nil to clear. It is meant to be set
// once during startup, before any SafeGo goroutine is launched.
func SetPanicHook(fn func(component string, recovered any)) {
	panicHook = fn
}

// SafeGo runs fn in a new goroutine guarded by panic recovery. A panic is
// logged with its stack under the given component and routed to the panic
// hook instead of crashing the whole daemon. Use this for every detached or
// long-lived goroutine in production code.
func SafeGo(component string, fn func()) {
	go RunSafely(component, fn)
}

// RunSafely runs fn synchronously with the same recovery as SafeGo. Use it
// inside a goroutine you already own (e.g. a worker-pool body) where the `go`
// statement is elsewhere.
func RunSafely(component string, fn func()) {
	defer Recover(component)
	fn()
}

// Recover recovers a panic in the current goroutine, logs it with a stack
// trace under component, and forwards it to the panic hook. Intended as
// `defer logging.Recover("component")` at the top of a goroutine body.
func Recover(component string) {
	if r := recover(); r != nil {
		Error("recovered panic in goroutine",
			"component", component,
			"panic", r,
			"stack", string(debug.Stack()))
		if panicHook != nil {
			panicHook(component, r)
		}
	}
}
