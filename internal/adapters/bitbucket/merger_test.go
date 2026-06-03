package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMergeWaitResult(t *testing.T) {
	tests := []struct {
		name   string
		result MergeWaitResult
	}{
		{
			name: "merged result",
			result: MergeWaitResult{
				Merged:   true,
				PRNumber: 42,
				PRURL:    "https://bitbucket.org/ws/repo/pull-requests/42",
				Message:  "PR #42 was merged",
			},
		},
		{
			name: "declined result",
			result: MergeWaitResult{
				Declined: true,
				PRNumber: 42,
				Message:  "PR #42 was declined without merging",
			},
		},
		{
			name: "timed out",
			result: MergeWaitResult{
				TimedOut: true,
				PRNumber: 42,
				Message:  "Timed out waiting for PR #42 to merge after 1h0m0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.PRNumber != 42 {
				t.Errorf("PRNumber = %d, want 42", tt.result.PRNumber)
			}
		})
	}
}

func TestDefaultMergeWaiterConfig(t *testing.T) {
	config := DefaultMergeWaiterConfig()
	if config.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", config.PollInterval)
	}
	if config.Timeout != 1*time.Hour {
		t.Errorf("Timeout = %v, want 1h", config.Timeout)
	}
}

func TestNewMergeWaiter(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")

	waiter := NewMergeWaiter(client, nil)
	if waiter == nil {
		t.Fatal("NewMergeWaiter returned nil")
	}
	if waiter.config.PollInterval != 30*time.Second {
		t.Errorf("default PollInterval = %v, want 30s", waiter.config.PollInterval)
	}

	customConfig := &MergeWaiterConfig{
		PollInterval: 10 * time.Second,
		Timeout:      30 * time.Minute,
	}
	waiter = NewMergeWaiter(client, customConfig)
	if waiter.config.PollInterval != 10*time.Second {
		t.Errorf("custom PollInterval = %v, want 10s", waiter.config.PollInterval)
	}
	if waiter.config.Timeout != 30*time.Minute {
		t.Errorf("custom Timeout = %v, want 30m", waiter.config.Timeout)
	}
}

func TestMergeWaiterErrors(t *testing.T) {
	if !errors.Is(ErrPRDeclined, ErrPRDeclined) {
		t.Error("ErrPRDeclined should be equal to itself")
	}
	if !errors.Is(ErrMergeTimeout, ErrMergeTimeout) {
		t.Error("ErrMergeTimeout should be equal to itself")
	}
	if !errors.Is(ErrBuildFailed, ErrBuildFailed) {
		t.Error("ErrBuildFailed should be equal to itself")
	}
	if ErrPRDeclined.Error() == "" || ErrMergeTimeout.Error() == "" || ErrBuildFailed.Error() == "" {
		t.Error("merge waiter errors should have non-empty messages")
	}
}

// TestWaitWithCallbackMerged drives the merge waiter against a server that
// reports a MERGED PR on the first poll, so it returns immediately.
func TestWaitWithCallbackMerged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pullrequests/7") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PullRequest{
			ID:    7,
			State: PRStateMerged,
			Links: &Links{HTML: &Link{Href: "https://bitbucket.org/ws/repo/pull-requests/7"}},
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	waiter := NewMergeWaiter(client, &MergeWaiterConfig{
		PollInterval: 10 * time.Millisecond,
		Timeout:      time.Second,
	})

	var polled bool
	result, err := waiter.WaitWithCallback(context.Background(), 7, func(r *MergeWaitResult) {
		polled = true
	})
	if err != nil {
		t.Fatalf("WaitWithCallback() error = %v", err)
	}
	if !result.Merged {
		t.Errorf("result.Merged = false, want true")
	}
	if !polled {
		t.Error("expected onPoll callback to be invoked")
	}
}

// TestWaitWithCallbackDeclined verifies a DECLINED PR is reported as declined.
func TestWaitWithCallbackDeclined(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PullRequest{ID: 8, State: PRStateDeclined})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(fakeToken, "ws", "repo", server.URL)
	waiter := NewMergeWaiter(client, &MergeWaiterConfig{
		PollInterval: 10 * time.Millisecond,
		Timeout:      time.Second,
	})

	result, err := waiter.WaitWithCallback(context.Background(), 8, nil)
	if err != nil {
		t.Fatalf("WaitWithCallback() error = %v", err)
	}
	if !result.Declined {
		t.Errorf("result.Declined = false, want true")
	}
}
