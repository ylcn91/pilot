package main

import (
	"strings"
	"testing"

	"github.com/qf-studio/pilot/internal/codexruntime"
)

func TestChatPromptFromArgs(t *testing.T) {
	got, err := chatPrompt([]string{"review", "this"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if got != "review this" {
		t.Fatalf("prompt = %q, want review this", got)
	}
}

func TestParseChatSandbox(t *testing.T) {
	got, err := parseChatSandbox("workspace-write")
	if err != nil {
		t.Fatal(err)
	}
	if got != codexruntime.SandboxWorkspaceWrite {
		t.Fatalf("sandbox = %q", got)
	}

	if _, err := parseChatSandbox("invalid"); err == nil {
		t.Fatal("expected invalid sandbox error")
	}
}
