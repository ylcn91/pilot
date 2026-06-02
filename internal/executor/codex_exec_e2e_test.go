package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexExecBackendE2E(t *testing.T) {
	codexPath := requireCodexE2E(t)

	const want = "pilot-codex-exec-e2e-ok"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var events []BackendEvent
	backend := NewCodexExecBackend(&CodexExecConfig{
		Command:   codexPath,
		Sandbox:   "read-only",
		Ephemeral: true,
	})

	result, err := backend.Execute(ctx, ExecuteOptions{
		ProjectPath: repoRoot(t),
		Prompt:      "Reply exactly: " + want,
		EventHandler: func(event BackendEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("CodexExecBackend.Execute() error = %v\nstderr:\n%s", err, stderrFromResult(result))
	}
	if result == nil {
		t.Fatal("CodexExecBackend.Execute() returned nil result")
	}
	if !result.Success {
		t.Fatalf("Success = false, stderr:\n%s", result.Stderr)
	}
	if got := strings.TrimSpace(result.Output); got != want {
		t.Fatalf("Output = %q, want %q\nlast assistant text: %q\nstderr:\n%s", got, want, result.LastAssistantText, result.Stderr)
	}
	if result.SessionID == "" {
		t.Fatal("SessionID is empty; expected codex exec thread.started event")
	}
	if !sawBackendEventType(events, EventTypeText) {
		t.Fatalf("expected at least one text event, got %#v", events)
	}
	if !sawBackendEventType(events, EventTypeResult) {
		t.Fatalf("expected final result event, got %#v", events)
	}
}

func requireCodexE2E(t *testing.T) string {
	t.Helper()

	if os.Getenv("PILOT_CODEX_E2E") != "1" {
		t.Skip("set PILOT_CODEX_E2E=1 to run real Codex CLI integration tests")
	}

	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLI not found in PATH")
	}
	return path
}

func sawBackendEventType(events []BackendEvent, eventType BackendEventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("repo root %q does not look like a git checkout: %v", root, err)
	}
	return root
}

func stderrFromResult(result *BackendResult) string {
	if result == nil {
		return ""
	}
	return result.Stderr
}
