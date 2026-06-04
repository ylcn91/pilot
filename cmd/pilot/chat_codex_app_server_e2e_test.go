package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

func TestRunChatCodexAppServerE2E(t *testing.T) {
	codexPath := requireCodexE2E(t)

	const want = "pilot-codex-app-server-e2e-ok"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runChat(ctx, chatOptions{
		cwd:     repoRoot(t),
		command: codexPath,
		sandbox: string(codexruntime.SandboxReadOnly),
		prompt:  "Reply exactly: " + want,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runChat() error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("stdout = %q, want %q\nstderr:\n%s", got, want, stderr.String())
	}
}

func TestChatAppServerArgs(t *testing.T) {
	if got := chatAppServerArgs(""); got != nil {
		t.Fatalf("chatAppServerArgs(empty) = %#v, want nil", got)
	}
	got := chatAppServerArgs("work")
	want := []string{"--profile", "work", "app-server", "--stdio"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("chatAppServerArgs() = %#v, want %#v", got, want)
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
