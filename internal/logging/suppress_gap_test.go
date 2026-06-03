package logging

import (
	"bytes"
	"log/slog"
	"testing"
)

// TestSuppressSilencesSubsequentLogs verifies that after Suppress() neither the
// package-level Info() nor a direct slog.Info() reaches a previously installed
// sink. We install a buffer-backed logger as both the package logger and the
// global slog default, snapshot what they would have produced, then suppress
// and assert the buffer stays empty.
func TestSuppressSilencesSubsequentLogs(t *testing.T) {
	// Snapshot and restore global logging state so this test does not leak
	// into other tests in the package.
	loggerMu.Lock()
	origDefault := defaultLogger
	loggerMu.Unlock()
	origSlogDefault := slog.Default()
	t.Cleanup(func() {
		loggerMu.Lock()
		defaultLogger = origDefault
		loggerMu.Unlock()
		slog.SetDefault(origSlogDefault)
	})

	var buf bytes.Buffer
	bufLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	loggerMu.Lock()
	defaultLogger = bufLogger
	loggerMu.Unlock()
	slog.SetDefault(bufLogger)

	// Sanity: before suppression the buffer-backed logger DOES emit.
	Info("pre-suppress message")
	if buf.Len() == 0 {
		t.Fatal("expected buffer logger to emit before Suppress (test setup invalid)")
	}
	buf.Reset()

	Suppress()

	// After Suppress, the package logger should be the discard logger.
	if Logger() == bufLogger {
		t.Error("Suppress did not replace the package default logger")
	}

	// Subsequent logging through any path must not reach the old sink.
	Info("post-suppress package info")
	Warn("post-suppress package warn")
	Error("post-suppress package error")
	slog.Info("post-suppress direct slog info")
	slog.Warn("post-suppress direct slog warn")

	if buf.Len() != 0 {
		t.Errorf("expected no output to the old sink after Suppress, got: %q", buf.String())
	}
}
