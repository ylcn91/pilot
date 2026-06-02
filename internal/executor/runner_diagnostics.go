package executor

import (
	"log/slog"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/replay"
)

// saveLogEntry writes a structured log entry to the log store (fire-and-forget).
func (r *Runner) saveLogEntry(executionID, level, message string) {
	if r.logStore == nil {
		return
	}
	if err := r.logStore.SaveLogEntry(&memory.LogEntry{
		ExecutionID: executionID,
		Timestamp:   time.Now(),
		Level:       level,
		Message:     message,
		Component:   "executor",
	}); err != nil {
		r.log.Warn("Failed to save log entry",
			slog.String("execution_id", executionID),
			slog.Any("error", err),
		)
	}
}

// Diagnostic truncation caps used by persistBackendDiagnostics. Exposed as
// constants so tests can assert the ceiling and project-side tooling can
// depend on a fixed upper bound. GH-2328.
const (
	diagnosticsStderrMaxChars  = 16 * 1024
	diagnosticsMessageMaxChars = 4 * 1024
)

// truncateDiagnostic trims `s` to at most `max` characters, appending a
// "\n[...truncated]" marker when truncation occurs. Callers should TrimSpace
// the input first so empty messages don't hit the log store.
func truncateDiagnostic(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n[...truncated]"
}

// parseDeclinedReason extracts the reason from a DECLINED:<reason> marker
// emitted by Claude when a task is explicitly unactionable. Returns the reason
// and true if found, or ("", false) if no marker is present. GH-2777.
func parseDeclinedReason(text string) (string, bool) {
	const marker = "DECLINED:"
	idx := strings.Index(text, marker)
	if idx == -1 {
		return "", false
	}
	reason := strings.TrimSpace(text[idx+len(marker):])
	// Trim to the first newline so we don't swallow prose that follows.
	if nl := strings.Index(reason, "\n"); nl != -1 {
		reason = strings.TrimSpace(reason[:nl])
	}
	if reason == "" {
		return "", false
	}
	return reason, true
}

// persistBackendDiagnostics writes the backend's stderr, error type, and final
// assistant text to execution_logs so `unknown: exit status 1` failures are
// actually diagnosable. Previously these bytes were only emitted via slog to
// stdout and disappeared when Pilot restarted. GH-2328.
func (r *Runner) persistBackendDiagnostics(executionID string, backendResult *BackendResult) {
	if backendResult == nil || r.logStore == nil {
		return
	}

	if backendResult.ErrorType != "" {
		r.saveLogEntry(executionID, "error",
			"Backend error classification: "+backendResult.ErrorType)
	}

	if stderr := strings.TrimSpace(backendResult.Stderr); stderr != "" {
		r.saveLogEntry(executionID, "error",
			"Backend stderr:\n"+truncateDiagnostic(stderr, diagnosticsStderrMaxChars))
	}

	if msg := strings.TrimSpace(backendResult.LastAssistantText); msg != "" {
		r.saveLogEntry(executionID, "error",
			"Final assistant message:\n"+truncateDiagnostic(msg, diagnosticsMessageMaxChars))
	}
}

// getRecordingsPath returns the recordings path, using default if not set
func (r *Runner) getRecordingsPath() string {
	if r.recordingsPath != "" {
		return r.recordingsPath
	}
	return replay.DefaultRecordingsPath()
}
