package bitbucket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// commentServer returns an httptest server that captures the raw comment body
// posted to the issue comments endpoint.
func commentServer(t *testing.T, captured *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/issues/") || !strings.Contains(r.URL.Path, "/comments") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body struct {
			Content struct {
				Raw string `json:"raw"`
			} `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*captured = append(*captured, body.Content.Raw)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Comment{ID: 1})
	}))
}

func TestNotifierLifecycle(t *testing.T) {
	var captured []string
	server := commentServer(t, &captured)
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	n := NewNotifier(client, "pilot")
	ctx := context.Background()

	if err := n.NotifyTaskStarted(ctx, 42, "BB-42"); err != nil {
		t.Fatalf("NotifyTaskStarted() error = %v", err)
	}
	if err := n.NotifyProgress(ctx, 42, "implementing", "writing code"); err != nil {
		t.Fatalf("NotifyProgress() error = %v", err)
	}
	if err := n.NotifyTaskCompleted(ctx, 42, "https://bitbucket.org/ws/repo/pull-requests/7", "done"); err != nil {
		t.Fatalf("NotifyTaskCompleted() error = %v", err)
	}
	if err := n.NotifyTaskFailed(ctx, 42, "build broke"); err != nil {
		t.Fatalf("NotifyTaskFailed() error = %v", err)
	}
	if err := n.LinkPR(ctx, 42, 7, "https://bitbucket.org/ws/repo/pull-requests/7"); err != nil {
		t.Fatalf("LinkPR() error = %v", err)
	}

	if len(captured) != 5 {
		t.Fatalf("captured %d comments, want 5", len(captured))
	}
	if !strings.Contains(captured[0], "BB-42") {
		t.Errorf("start comment missing task ID: %s", captured[0])
	}
	if !strings.Contains(captured[2], "pull-requests/7") {
		t.Errorf("completion comment missing PR URL: %s", captured[2])
	}
	if !strings.Contains(captured[3], "build broke") {
		t.Errorf("failure comment missing reason: %s", captured[3])
	}
}
