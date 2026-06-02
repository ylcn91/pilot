package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPoller_HasMergedWork(t *testing.T) {
	tests := []struct {
		name           string
		searchResponse string
		wantSkip       bool
		wantLabeled    bool
	}{
		{
			name:           "merged PRs exist - skip and label",
			searchResponse: `{"total_count": 2}`,
			wantSkip:       true,
			wantLabeled:    true,
		},
		{
			name:           "no merged PRs - allow retry",
			searchResponse: `{"total_count": 0}`,
			wantSkip:       false,
			wantLabeled:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var labeled bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/search/issues":
					_, _ = w.Write([]byte(tt.searchResponse))
				case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/42/labels":
					labeled = true
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

			issue := &Issue{Number: 42, Title: "Test issue"}
			got := poller.hasMergedWork(context.Background(), issue)

			if got != tt.wantSkip {
				t.Errorf("hasMergedWork() = %v, want %v", got, tt.wantSkip)
			}
			if labeled != tt.wantLabeled {
				t.Errorf("labeled = %v, want %v", labeled, tt.wantLabeled)
			}
			if tt.wantSkip && !poller.IsProcessed(42) {
				t.Error("issue should be marked as processed when skipped")
			}
		})
	}
}

func TestPoller_HasMergedWork_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "rate limit"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue := &Issue{Number: 42, Title: "Test issue"}
	got := poller.hasMergedWork(context.Background(), issue)

	// Should not block on API errors — allow the issue through
	if got {
		t.Error("hasMergedWork() should return false on API error")
	}
}

// GH-2341: Search API may lag up to ~30s after a merge. hasMergedWork must
// supplement with a direct REST PR lookup by branch to catch that window.
func TestPoller_HasMergedWork_SearchLag_BranchFallback(t *testing.T) {
	var searchCalls, pullsCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/issues":
			atomic.AddInt32(&searchCalls, 1)
			_, _ = w.Write([]byte(`{"total_count": 0}`))
		case "/repos/owner/repo/pulls":
			atomic.AddInt32(&pullsCalls, 1)
			head := r.URL.Query().Get("head")
			if head != "owner:pilot/GH-42" {
				t.Errorf("unexpected head filter: %q", head)
			}
			_, _ = w.Write([]byte(`[{"number": 100, "merged_at": "2026-04-17T14:01:40Z", "head": {"ref": "pilot/GH-42"}}]`))
		case "/repos/owner/repo/issues/42/labels":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusOK)
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue := &Issue{Number: 42, Title: "Test issue"}
	got := poller.hasMergedWork(context.Background(), issue)

	if !got {
		t.Error("hasMergedWork() should return true when branch has merged PR (even with Search API empty)")
	}
	if atomic.LoadInt32(&pullsCalls) == 0 {
		t.Error("expected branch REST lookup to be called when Search API returned empty")
	}
	_ = searchCalls
}

// GH-2341: If neither Search API nor branch REST find a merged PR, allow retry.
func TestPoller_HasMergedWork_NoMerges_NoFallbackBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/issues":
			_, _ = w.Write([]byte(`{"total_count": 0}`))
		case "/repos/owner/repo/pulls":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue := &Issue{Number: 42, Title: "Test issue"}
	if poller.hasMergedWork(context.Background(), issue) {
		t.Error("hasMergedWork() should return false when no merged PRs exist on branch")
	}
}
