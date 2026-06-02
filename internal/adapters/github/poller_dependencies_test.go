package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestParseDependencies(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []int
	}{
		{
			name: "depends on with colon",
			body: "This issue depends on: #123",
			want: []int{123},
		},
		{
			name: "depends on without colon",
			body: "Depends on #456",
			want: []int{456},
		},
		{
			name: "blocked by with colon",
			body: "Blocked by: #789",
			want: []int{789},
		},
		{
			name: "blocked by without colon",
			body: "blocked by #101",
			want: []int{101},
		},
		{
			name: "requires with colon",
			body: "Requires: #202",
			want: []int{202},
		},
		{
			name: "requires without colon",
			body: "requires #303",
			want: []int{303},
		},
		{
			name: "multiple dependencies",
			body: "Depends on: #1\nBlocked by: #2\nRequires: #3",
			want: []int{1, 2, 3},
		},
		{
			name: "duplicate dependencies",
			body: "Depends on: #100\nAlso depends on #100",
			want: []int{100},
		},
		{
			name: "markdown header format",
			body: "## Depends on: #555",
			want: []int{555},
		},
		{
			name: "case insensitive",
			body: "DEPENDS ON: #111\nBLOCKED BY: #222",
			want: []int{111, 222},
		},
		{
			name: "no dependencies",
			body: "Just a regular issue body with #123 mentioned but not as dependency",
			want: nil,
		},
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "mixed content",
			body: "## Task\nImplement feature X\n\nDepends on: #42\n\nSee also #99 (not a dependency)",
			want: []int{42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDependencies(tt.body)

			if len(got) != len(tt.want) {
				t.Errorf("ParseDependencies() returned %d deps, want %d", len(got), len(tt.want))
				t.Errorf("got: %v, want: %v", got, tt.want)
				return
			}

			// Check each dependency is present (order may vary due to regex matching)
			gotMap := make(map[int]bool)
			for _, d := range got {
				gotMap[d] = true
			}
			for _, w := range tt.want {
				if !gotMap[w] {
					t.Errorf("ParseDependencies() missing dependency #%d", w)
				}
			}
		})
	}
}

func TestPoller_HasPendingDependencies(t *testing.T) {
	tests := []struct {
		name      string
		issueBody string
		depState  string // "open" or "closed"
		want      bool
	}{
		{
			name:      "no dependencies",
			issueBody: "Regular issue body",
			depState:  "",
			want:      false,
		},
		{
			name:      "dependency is open",
			issueBody: "Depends on: #100",
			depState:  "open",
			want:      true,
		},
		{
			name:      "dependency is closed",
			issueBody: "Depends on: #100",
			depState:  "closed",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/repos/owner/repo/issues/100" {
					issue := &Issue{Number: 100, State: tt.depState}
					_ = json.NewEncoder(w).Encode(issue)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

			issue := &Issue{Number: 1, Body: tt.issueBody}
			got := poller.hasPendingDependencies(context.Background(), issue)

			if got != tt.want {
				t.Errorf("hasPendingDependencies() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPoller_HasPendingDependencies_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue := &Issue{Number: 1, Body: "Depends on: #100"}
	got := poller.hasPendingDependencies(context.Background(), issue)

	// Should return true (has pending) when API fails - be safe and don't execute
	if !got {
		t.Error("hasPendingDependencies() should return true on API error")
	}
}

func TestPoller_FindOldestUnprocessedIssue_SkipsPendingDependencies(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Oldest with dep", Body: "Depends on: #100", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Second no dep", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case "/repos/owner/repo/issues/100":
			// Dependency is still open
			_ = json.NewEncoder(w).Encode(&Issue{Number: 100, State: "open"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())

	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil")
	}
	// Should skip #1 (has open dependency) and return #2
	if issue.Number != 2 {
		t.Errorf("found issue #%d, want #2 (skips issue with open dependency)", issue.Number)
	}
}

func TestPoller_FindOldestUnprocessedIssue_PicksClosedDependency(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Oldest with closed dep", Body: "Depends on: #100", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Second no dep", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case "/repos/owner/repo/issues/100":
			// Dependency is closed
			_ = json.NewEncoder(w).Encode(&Issue{Number: 100, State: "closed"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())

	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if issue == nil {
		t.Fatal("issue should not be nil")
	}
	// Should pick #1 because dependency is closed
	if issue.Number != 1 {
		t.Errorf("found issue #%d, want #1 (dependency is closed)", issue.Number)
	}
}

func TestPoller_FindOldestUnprocessedIssue_AllDepsOpen(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Has dep", Body: "Depends on: #100", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 2, Title: "Also has dep", Body: "Blocked by: #101", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			_ = json.NewEncoder(w).Encode(issues)
		case "/repos/owner/repo/issues/100", "/repos/owner/repo/issues/101":
			// Both dependencies are open
			_ = json.NewEncoder(w).Encode(&Issue{Number: 100, State: "open"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	issue, err := poller.findOldestUnprocessedIssue(context.Background())

	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	// Should return nil when all issues have open dependencies
	if issue != nil {
		t.Errorf("issue should be nil when all have open dependencies, got #%d", issue.Number)
	}
}

// GH-1355: Test recovery of orphaned in-progress issues
func TestPoller_RecoverOrphanedIssues(t *testing.T) {
	// Mock issues that have both pilot and pilot-in-progress labels
	orphanedIssues := []*Issue{
		{Number: 123, Title: "Orphaned Issue 1", Labels: []Label{{Name: "pilot"}, {Name: LabelInProgress}}},
		{Number: 456, Title: "Orphaned Issue 2", Labels: []Label{{Name: "pilot"}, {Name: LabelInProgress}}},
	}

	removedLabels := []int{}
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == "/repos/owner/repo/issues" {
				_ = json.NewEncoder(w).Encode(orphanedIssues)
			}
		case http.MethodDelete:
			// Track label removal calls
			switch r.URL.Path {
			case "/repos/owner/repo/issues/123/labels/pilot-in-progress":
				mu.Lock()
				removedLabels = append(removedLabels, 123)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
			case "/repos/owner/repo/issues/456/labels/pilot-in-progress":
				mu.Lock()
				removedLabels = append(removedLabels, 456)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
			}
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Call recovery directly
	poller.recoverOrphanedIssues(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// Should have removed labels from both orphaned issues
	if len(removedLabels) != 2 {
		t.Errorf("expected 2 labels removed, got %d", len(removedLabels))
	}
}

// GH-1355: Test recovery handles empty result
func TestPoller_RecoverOrphanedIssues_NoOrphanedIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return empty array - no orphaned issues
		_ = json.NewEncoder(w).Encode([]*Issue{})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Should not panic or error with empty result
	poller.recoverOrphanedIssues(context.Background())
}

// GH-1355: Test recovery handles API error gracefully
func TestPoller_RecoverOrphanedIssues_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second)

	// Should not panic - errors are logged but not returned
	poller.recoverOrphanedIssues(context.Background())
}
