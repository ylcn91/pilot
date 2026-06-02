package codexruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTypedThreadStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}

	script := filepath.Join(t.TempDir(), "fake-app-server.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  if [ -n "$id" ]; then
    printf '{"id":%s,"result":{"thread":{"id":"thread-1","sessionId":"session-1","preview":"","ephemeral":true,"status":{"type":"idle"},"cwd":"/tmp"},"model":"gpt-test","modelProvider":"openai","serviceTier":null,"cwd":"/tmp","approvalPolicy":"never","approvalsReviewer":"user","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$id"
  fi
done
`), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Start(ctx, Config{Command: script})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ephemeral := true
	resp, err := client.ThreadStart(ctx, ThreadStartParams{
		Cwd:                "/tmp",
		ApprovalPolicy:     ApprovalNever,
		ApprovalsReviewer:  ApprovalsReviewerUser,
		Sandbox:            SandboxReadOnly,
		Ephemeral:          &ephemeral,
		ThreadSource:       ThreadSourceUser,
		SessionStartSource: ThreadStartSourceStartup,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Thread.ID != "thread-1" {
		t.Fatalf("thread id = %q, want thread-1", resp.Thread.ID)
	}
	if resp.Model != "gpt-test" {
		t.Fatalf("model = %q, want gpt-test", resp.Model)
	}
}

func TestTextUserInput(t *testing.T) {
	input := TextUserInput("hello")

	if input.Type != "text" {
		t.Fatalf("type = %q, want text", input.Type)
	}
	if input.Text != "hello" {
		t.Fatalf("text = %q, want hello", input.Text)
	}
	if input.TextElements == nil {
		t.Fatal("text elements should be an empty slice, not nil")
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"text","text":"hello","text_elements":[]}`
	if string(data) != want {
		t.Fatalf("json = %s, want %s", data, want)
	}
}
