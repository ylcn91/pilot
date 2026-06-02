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

func TestRequestCorrelatesResponseAndPreservesNotifications(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}

	script := filepath.Join(t.TempDir(), "fake-app-server.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  if [ -n "$id" ]; then
    printf '{"method":"thread/status/changed","params":{"threadId":"thread-1","status":{"type":"active","activeFlags":[]}}}\n'
    printf '{"id":%s,"result":{"thread":{"id":"thread-1"}}}\n' "$id"
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

	msg, err := client.Request(ctx, "thread/start", map[string]any{"cwd": "."})
	if err != nil {
		t.Fatal(err)
	}

	threadID, err := ExtractString(msg.Result, "thread", "id")
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "thread-1" {
		t.Fatalf("thread id = %q, want thread-1", threadID)
	}

	select {
	case notification := <-client.Notifications():
		if notification.Method != "thread/status/changed" {
			t.Fatalf("notification method = %q, want thread/status/changed", notification.Method)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "number", raw: `3`, want: 3},
		{name: "string", raw: `"7"`, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseID(json.RawMessage(tt.raw))
			if err != nil {
				t.Fatalf("ParseID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseID() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractString(t *testing.T) {
	raw := json.RawMessage(`{"thread":{"id":"thread-1"},"turn":{"id":"turn-1"}}`)

	got, err := ExtractString(raw, "thread", "id")
	if err != nil {
		t.Fatalf("ExtractString() error = %v", err)
	}
	if got != "thread-1" {
		t.Fatalf("ExtractString() = %q, want thread-1", got)
	}
}
