package executor

import (
	"errors"
	"log/slog"
	"testing"
)

func newResultTestBackend() *ClaudeCodeBackend {
	return &ClaudeCodeBackend{config: &ClaudeCodeConfig{Command: "claude"}, log: slog.Default()}
}

func TestFinalizeResult_Success(t *testing.T) {
	b := newResultTestBackend()
	in := &BackendResult{Output: "done"}
	out, err := b.finalizeResult(in, nil, "warn line")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Success {
		t.Error("Success should be true")
	}
	if out.Stderr != "warn line" {
		t.Errorf("Stderr = %q, want %q", out.Stderr, "warn line")
	}
}

func TestFinalizeResult_GH2107Recovery(t *testing.T) {
	b := newResultTestBackend()
	in := &BackendResult{Output: "work done", SawSuccessResult: true}
	out, err := b.finalizeResult(in, errors.New("exit status 1"), "")
	if err != nil {
		t.Fatalf("recovery should swallow error, got %v", err)
	}
	if !out.Success {
		t.Error("Success should be true after GH-2107 recovery")
	}
	if out.Output != "work done" {
		t.Errorf("Output = %q, want preserved", out.Output)
	}
}

func TestFinalizeResult_ErrorUnknown(t *testing.T) {
	b := newResultTestBackend()
	in := &BackendResult{}
	out, err := b.finalizeResult(in, errors.New("exit status 1"), "boom")
	if err == nil {
		t.Fatal("expected error")
	}
	var ccErr *ClaudeCodeError
	if !errors.As(err, &ccErr) {
		t.Fatalf("expected *ClaudeCodeError, got %T", err)
	}
	if out.Success {
		t.Error("Success should be false")
	}
	if out.ErrorType != string(ErrorTypeUnknown) {
		t.Errorf("ErrorType = %q, want %q", out.ErrorType, ErrorTypeUnknown)
	}
	if out.Stderr != "boom" {
		t.Errorf("Stderr = %q, want boom", out.Stderr)
	}
}

func TestFinalizeResult_ErrorClassifiedFromStderr(t *testing.T) {
	b := newResultTestBackend()
	in := &BackendResult{}
	out, err := b.finalizeResult(in, errors.New("exit status 1"), "session not found")
	if err == nil {
		t.Fatal("expected error")
	}
	if out.ErrorType != string(ErrorTypeSessionNotFound) {
		t.Errorf("ErrorType = %q, want %q", out.ErrorType, ErrorTypeSessionNotFound)
	}
}

func TestFinalizeResult_PreservesExistingError(t *testing.T) {
	b := newResultTestBackend()
	in := &BackendResult{Error: "preset error"}
	out, _ := b.finalizeResult(in, errors.New("exit status 1"), "boom")
	if out.Error != "preset error" {
		t.Errorf("Error = %q, want preset preserved", out.Error)
	}
}
