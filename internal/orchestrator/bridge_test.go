package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestRunPython_MissingInterpreter verifies that pointing the bridge at a
// non-existent interpreter fails fast with ErrPythonNotFound rather than
// hanging or returning an opaque error. No real python is involved.
func TestRunPython_MissingInterpreter(t *testing.T) {
	b := &Bridge{
		pythonPath: filepath.Join(t.TempDir(), "does-not-exist-python"),
		scriptDir:  t.TempDir(),
	}

	start := time.Now()
	_, err := b.runPython(context.Background(), "print('hi')")
	if err == nil {
		t.Fatal("expected an error when the interpreter is missing")
	}
	if !errors.Is(err, ErrPythonNotFound) {
		t.Fatalf("expected ErrPythonNotFound, got %v", err)
	}
	// "Fast" means it does not wait out the subprocess timeout.
	if elapsed := time.Since(start); elapsed >= pythonTimeout {
		t.Fatalf("missing interpreter took %s, expected a fast failure", elapsed)
	}
}

// TestPlanTicket_MissingInterpreter verifies the missing-python error is
// surfaced through the public PlanTicket entrypoint (the queue-worker path).
func TestPlanTicket_MissingInterpreter(t *testing.T) {
	b := &Bridge{
		pythonPath: filepath.Join(t.TempDir(), "does-not-exist-python"),
		scriptDir:  t.TempDir(),
	}

	_, err := b.PlanTicket(context.Background(), &TicketData{Identifier: "ABC-1", Title: "t"})
	if err == nil {
		t.Fatal("expected PlanTicket to fail when the interpreter is missing")
	}
	if !errors.Is(err, ErrPythonNotFound) {
		t.Fatalf("expected ErrPythonNotFound, got %v", err)
	}
}

// TestRunPython_Timeout verifies that a slow subprocess is bounded by the
// context deadline instead of blocking the caller indefinitely. The stub
// sleeps well past the (short) deadline the test imposes; no real python.
func TestRunPython_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub is POSIX-only")
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "python-sleep.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write sleeping stub: %v", err)
	}

	b := &Bridge{pythonPath: stub, scriptDir: t.TempDir()}

	// A deadline far shorter than pythonTimeout proves the call returns when
	// the (parent) context expires rather than running the full sleep.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := b.runPython(ctx, "ignored")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error from a slow subprocess")
	}
	if errors.Is(err, ErrPythonNotFound) {
		t.Fatalf("slow subprocess misclassified as missing interpreter: %v", err)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("runPython took %s, deadline was not enforced", elapsed)
	}
}
