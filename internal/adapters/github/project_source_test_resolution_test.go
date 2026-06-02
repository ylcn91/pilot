package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestFindIssuesFromProject_DefaultStatusField(t *testing.T) {
	var capturedVars map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if strings.Contains(req.Query, "organization") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(orgProjectResp("PVT_default")))
			return
		}
		capturedVars = req.Variables
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(itemsResp(nil, false, "")))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	// StatusField intentionally empty — should default to "Status".
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 6,
	}, "org", "repo")

	_, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if capturedVars["statusField"] != "Status" {
		t.Errorf("expected statusField='Status', got %v", capturedVars["statusField"])
	}
}

func TestFindIssuesFromProject_CachesProjectID(t *testing.T) {
	resolveCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(req.Query, "organization") {
			resolveCount++
			_, _ = w.Write([]byte(orgProjectResp("PVT_cached")))
			return
		}
		_, _ = w.Write([]byte(itemsResp(nil, false, "")))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 7,
		StatusField:   "Status",
	}, "org", "repo")

	for i := 0; i < 3; i++ {
		if _, err := src.FindIssuesFromProject(context.Background(), "Todo"); err != nil {
			t.Fatalf("call %d: error = %v", i, err)
		}
	}
	if resolveCount != 1 {
		t.Errorf("expected project ID resolved once, got %d", resolveCount)
	}
}

func TestFindIssuesFromProject_CreatedAtPopulated(t *testing.T) {
	ts := "2024-03-15T10:30:00Z"
	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_cat"),
		itemsResp([]map[string]interface{}{
			issueNodeWithCreatedAt(50, "I_50", "With time", "", "OPEN", "org/repo", "Todo", ts),
			issueNode(51, "I_51", "No time", "", "OPEN", "org/repo", "Todo"),
		}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 10,
		StatusField:   "Status",
	}, "org", "repo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	want, _ := time.Parse(time.RFC3339, ts)
	if !issues[0].CreatedAt.Equal(want) {
		t.Errorf("issue #50 CreatedAt = %v, want %v", issues[0].CreatedAt, want)
	}
	if !issues[1].CreatedAt.IsZero() {
		t.Errorf("issue #51 CreatedAt should be zero (no createdAt in response), got %v", issues[1].CreatedAt)
	}
}

func TestFindIssuesFromProject_OldestFirstOrdering(t *testing.T) {
	older := "2024-01-01T00:00:00Z"
	newer := "2024-06-01T00:00:00Z"

	// GraphQL returns newer first (board insertion order).
	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_order"),
		itemsResp([]map[string]interface{}{
			issueNodeWithCreatedAt(200, "I_200", "Newer", "", "OPEN", "org/repo", "Todo", newer),
			issueNodeWithCreatedAt(100, "I_100", "Older", "", "OPEN", "org/repo", "Todo", older),
		}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 11,
		StatusField:   "Status",
	}, "org", "repo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	olderTime, _ := time.Parse(time.RFC3339, older)
	newerTime, _ := time.Parse(time.RFC3339, newer)

	// Both CreatedAt fields must be populated so the poller's oldest-first sort works.
	// GraphQL returns them in board/insertion order (#200 then #100); verify timestamps match.
	if !issues[0].CreatedAt.Equal(newerTime) {
		t.Errorf("issues[0] (#200) CreatedAt = %v, want newer (%v)", issues[0].CreatedAt, newerTime)
	}
	if !issues[1].CreatedAt.Equal(olderTime) {
		t.Errorf("issues[1] (#100) CreatedAt = %v, want older (%v)", issues[1].CreatedAt, olderTime)
	}

	// Applying the same sort the poller uses gives oldest-first order.
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].CreatedAt.Before(issues[j].CreatedAt)
	})
	if issues[0].Number != 100 || issues[1].Number != 200 {
		t.Errorf("oldest-first sort: expected [100, 200], got [%d, %d]", issues[0].Number, issues[1].Number)
	}
}

// TestSourceEnabledFalse_UsesLabelPath is a regression guard: when source_enabled=false
// (i.e. projectBoardSource is nil), findOldestUnprocessedIssue must reach the
// label-based REST path (ListIssues), not the board GraphQL path.
func TestSourceEnabledFalse_UsesLabelPath(t *testing.T) {
	restCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			restCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		t.Errorf("unexpected request to %s (board GraphQL called despite source_enabled=false)", r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	poller, err := NewPoller(client, "org/repo", "pilot", 0)
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}
	// projectBoardSource is nil → must use label path
	if poller.projectBoardSource != nil {
		t.Fatal("expected projectBoardSource to be nil when not configured")
	}

	_, err = poller.findOldestUnprocessedIssue(context.Background())
	if err != nil {
		t.Fatalf("findOldestUnprocessedIssue() error = %v", err)
	}
	if !restCalled {
		t.Error("expected REST /repos/... to be called for label path, but it wasn't")
	}
}
