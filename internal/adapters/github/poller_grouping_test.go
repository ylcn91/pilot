package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestGroupByOverlappingScope(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		issues         []*Issue
		wantGroups     int
		wantMaxGroupSz int
	}{
		{
			name:       "empty input",
			issues:     nil,
			wantGroups: 0,
		},
		{
			name: "single issue",
			issues: []*Issue{
				{Number: 1, Body: "Modify internal/comms/handler.go"},
			},
			wantGroups:     1,
			wantMaxGroupSz: 1,
		},
		{
			name: "three issues same directory form one group",
			issues: []*Issue{
				{Number: 1, Body: "Modify internal/comms/handler.go", CreatedAt: now.Add(-3 * time.Hour)},
				{Number: 2, Body: "Update internal/comms/router.go", CreatedAt: now.Add(-2 * time.Hour)},
				{Number: 3, Body: "Refactor internal/comms/types.go", CreatedAt: now.Add(-1 * time.Hour)},
			},
			wantGroups:     1,
			wantMaxGroupSz: 3,
		},
		{
			name: "three issues different directories form three groups",
			issues: []*Issue{
				{Number: 1, Body: "Modify internal/gateway/server.go", CreatedAt: now.Add(-3 * time.Hour)},
				{Number: 2, Body: "Update internal/executor/runner.go", CreatedAt: now.Add(-2 * time.Hour)},
				{Number: 3, Body: "Refactor internal/adapters/slack.go", CreatedAt: now.Add(-1 * time.Hour)},
			},
			wantGroups:     3,
			wantMaxGroupSz: 1,
		},
		{
			name: "mixed overlap: two overlap plus one independent",
			issues: []*Issue{
				{Number: 1, Body: "Modify internal/comms/handler.go", CreatedAt: now.Add(-3 * time.Hour)},
				{Number: 2, Body: "Update internal/comms/router.go", CreatedAt: now.Add(-2 * time.Hour)},
				{Number: 3, Body: "Refactor internal/gateway/server.go", CreatedAt: now.Add(-1 * time.Hour)},
			},
			wantGroups:     2,
			wantMaxGroupSz: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := groupByOverlappingScope(tt.issues)
			if len(groups) != tt.wantGroups {
				t.Errorf("got %d groups, want %d", len(groups), tt.wantGroups)
			}
			maxSz := 0
			for _, g := range groups {
				if len(g) > maxSz {
					maxSz = len(g)
				}
			}
			if tt.wantGroups > 0 && maxSz != tt.wantMaxGroupSz {
				t.Errorf("max group size = %d, want %d", maxSz, tt.wantMaxGroupSz)
			}
		})
	}
}

// GH-1806: 3 issues referencing internal/comms/ → only 1 dispatched
func TestPoller_OverlapGrouping_AllOverlap(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 3, Title: "Newest comms", Body: "Change internal/comms/types.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 1, Title: "Oldest comms", Body: "Change internal/comms/handler.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-3 * time.Hour)},
		{Number: 2, Title: "Middle comms", Body: "Change internal/comms/router.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var dispatched []int
	var mu sync.Mutex
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			mu.Lock()
			dispatched = append(dispatched, issue.Number)
			mu.Unlock()
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 1 {
		t.Fatalf("dispatched %d issues, want 1; got %v", len(dispatched), dispatched)
	}
	if dispatched[0] != 1 {
		t.Errorf("dispatched issue %d, want 1 (oldest)", dispatched[0])
	}
}

// GH-1806: 3 issues referencing different directories → all 3 dispatched
func TestPoller_OverlapGrouping_NoOverlap(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Gateway", Body: "Change internal/gateway/server.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-3 * time.Hour)},
		{Number: 2, Title: "Executor", Body: "Change internal/executor/runner.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 3, Title: "Adapters", Body: "Change internal/adapters/slack.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	if got := atomic.LoadInt32(&callCount); got != 3 {
		t.Errorf("dispatched %d issues, want 3", got)
	}
}

// GH-1806: Mixed overlap groups → correct subset dispatched
func TestPoller_OverlapGrouping_MixedGroups(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		// Group 1: internal/comms overlap (issues 1, 2)
		{Number: 1, Title: "Comms A", Body: "Change internal/comms/handler.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-3 * time.Hour)},
		{Number: 2, Title: "Comms B", Body: "Change internal/comms/router.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		// Group 2: independent (issue 3)
		{Number: 3, Title: "Gateway", Body: "Change internal/gateway/server.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var dispatched []int
	var mu sync.Mutex
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			mu.Lock()
			dispatched = append(dispatched, issue.Number)
			mu.Unlock()
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 2 {
		t.Fatalf("dispatched %d issues, want 2; got %v", len(dispatched), dispatched)
	}

	// Should dispatch issue 1 (oldest in comms group) and issue 3 (standalone)
	dispatchedSet := map[int]bool{}
	for _, n := range dispatched {
		dispatchedSet[n] = true
	}
	if !dispatchedSet[1] {
		t.Error("expected issue 1 (oldest in comms group) to be dispatched")
	}
	if !dispatchedSet[3] {
		t.Error("expected issue 3 (standalone gateway) to be dispatched")
	}
	if dispatchedSet[2] {
		t.Error("issue 2 should be deferred (overlaps with older issue 1)")
	}
}

// GH-1799: auto mode — non-overlapping issues dispatched in parallel
func TestPoller_AutoMode_NonOverlapping(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 1, Title: "Gateway", Body: "Change internal/gateway/server.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-3 * time.Hour)},
		{Number: 2, Title: "Executor", Body: "Change internal/executor/runner.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 3, Title: "Adapters", Body: "Change internal/adapters/slack.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var callCount int32
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithExecutionMode(ExecutionModeAuto),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	// All 3 non-overlapping issues should be dispatched in parallel
	if got := atomic.LoadInt32(&callCount); got != 3 {
		t.Errorf("auto mode dispatched %d issues, want 3 (non-overlapping should all run)", got)
	}
}

// GH-1799: auto mode — overlapping issues dispatch only the oldest
func TestPoller_AutoMode_OverlappingDispatchesOldestOnly(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 3, Title: "Newest comms", Body: "Change internal/comms/types.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 1, Title: "Oldest comms", Body: "Change internal/comms/handler.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-3 * time.Hour)},
		{Number: 2, Title: "Middle comms", Body: "Change internal/comms/router.go", Labels: []Label{{Name: "pilot"}}, CreatedAt: now.Add(-2 * time.Hour)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	var dispatched []int
	var mu sync.Mutex
	poller, _ := NewPoller(client, "owner/repo", "pilot", 30*time.Second,
		WithExecutionMode(ExecutionModeAuto),
		WithOnIssue(func(ctx context.Context, issue *Issue) error {
			mu.Lock()
			dispatched = append(dispatched, issue.Number)
			mu.Unlock()
			return nil
		}),
	)

	poller.checkForNewIssues(context.Background())
	poller.WaitForActive()

	mu.Lock()
	defer mu.Unlock()
	// Only the oldest (issue 1) should be dispatched; 2 and 3 deferred
	if len(dispatched) != 1 {
		t.Fatalf("auto mode dispatched %d issues, want 1; got %v", len(dispatched), dispatched)
	}
	if dispatched[0] != 1 {
		t.Errorf("auto mode dispatched issue %d, want 1 (oldest)", dispatched[0])
	}
}
