package codexruntime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestServerRequestsAreSeparatedFromResponses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}

	dir := t.TempDir()
	responsePath := filepath.Join(dir, "response.txt")
	script := filepath.Join(dir, "fake-app-server.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
out="$1"
printf '{"id":99,"method":"item/fileChange/requestApproval","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","startedAtMs":1}}\n'
IFS= read -r response
printf '%s\n' "$response" > "$out"
`), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Start(ctx, Config{Command: script, Args: []string{responsePath}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var request Message
	select {
	case request = <-client.ServerRequests():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if request.Method != "item/fileChange/requestApproval" {
		t.Fatalf("request method = %q", request.Method)
	}

	if err := client.RespondFileChangeApproval(request, FileChangeAccept); err != nil {
		t.Fatal(err)
	}

	response := waitForFile(t, ctx, responsePath)
	if !strings.Contains(response, `"id":99`) {
		t.Fatalf("response id missing: %s", response)
	}
	if !strings.Contains(response, `"decision":"accept"`) {
		t.Fatalf("approval decision missing: %s", response)
	}
}

func waitForFile(t *testing.T, ctx context.Context, path string) string {
	t.Helper()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
