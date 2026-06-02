package github

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestFindIssuesFromProject_SinglePage(t *testing.T) {
	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_proj1"),
		itemsResp([]map[string]interface{}{
			issueNode(10, "I_10", "Fix bug", "body10", "OPEN", "myorg/myrepo", "Todo", "pilot"),
			issueNode(11, "I_11", "Add feature", "body11", "OPEN", "myorg/myrepo", "In Progress"),
		}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 1,
		StatusField:   "Status",
	}, "myorg", "myrepo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (only Todo), got %d", len(issues))
	}
	if issues[0].Number != 10 {
		t.Errorf("expected issue #10, got #%d", issues[0].Number)
	}
	if issues[0].Title != "Fix bug" {
		t.Errorf("expected title 'Fix bug', got %q", issues[0].Title)
	}
	if issues[0].Body != "body10" {
		t.Errorf("expected body 'body10', got %q", issues[0].Body)
	}
	if issues[0].State != "open" {
		t.Errorf("expected state 'open', got %q", issues[0].State)
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0].Name != "pilot" {
		t.Errorf("expected label 'pilot', got %v", issues[0].Labels)
	}
}

func TestFindIssuesFromProject_Pagination(t *testing.T) {
	page1Node := issueNode(1, "I_1", "Issue 1", "", "OPEN", "org/repo", "Todo")
	page2Node := issueNode(2, "I_2", "Issue 2", "", "OPEN", "org/repo", "Todo")

	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_paged"),
		itemsResp([]map[string]interface{}{page1Node}, true, "cursor-abc"),
		itemsResp([]map[string]interface{}{page2Node}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 2,
		StatusField:   "Status",
	}, "org", "repo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues across 2 pages, got %d", len(issues))
	}
	if issues[0].Number != 1 || issues[1].Number != 2 {
		t.Errorf("unexpected issue numbers: %v, %v", issues[0].Number, issues[1].Number)
	}
}

func TestFindIssuesFromProject_CrossRepoExclusion(t *testing.T) {
	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_crossrepo"),
		itemsResp([]map[string]interface{}{
			issueNode(5, "I_5", "Same repo", "", "OPEN", "org/repo", "Todo"),
			issueNode(6, "I_6", "Other repo", "", "OPEN", "org/other-repo", "Todo"),
			issueNode(7, "I_7", "Different org", "", "OPEN", "other-org/repo", "Todo"),
		}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 3,
		StatusField:   "Status",
	}, "org", "repo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (cross-repo filtered), got %d: %v", len(issues), issues)
	}
	if issues[0].Number != 5 {
		t.Errorf("expected issue #5 to survive filter, got #%d", issues[0].Number)
	}
}

func TestFindIssuesFromProject_LabelHydration(t *testing.T) {
	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_labels"),
		itemsResp([]map[string]interface{}{
			issueNode(20, "I_20", "Labeled", "", "OPEN", "org/repo", "Todo", "pilot", "priority:high", "bug"),
		}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 4,
		StatusField:   "Status",
	}, "org", "repo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	gotLabels := make(map[string]bool)
	for _, l := range issues[0].Labels {
		gotLabels[l.Name] = true
	}
	for _, want := range []string{"pilot", "priority:high", "bug"} {
		if !gotLabels[want] {
			t.Errorf("expected label %q, not found in %v", want, issues[0].Labels)
		}
	}
}

func TestFindIssuesFromProject_ClosedIssueExclusion(t *testing.T) {
	cases := []struct {
		name        string
		state       string
		wantInclude bool
	}{
		{"open uppercase", "OPEN", true},
		{"open lowercase", "open", true},
		{"closed", "CLOSED", false},
		{"closed lowercase", "closed", false},
		{"merged", "MERGED", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := fakeProjectSourceServer(t, []string{
				orgProjectResp("PVT_closed"),
				itemsResp([]map[string]interface{}{
					issueNode(42, "I_42", "Some issue", "", tc.state, "org/repo", "Todo"),
				}, false, ""),
			})
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			src := NewProjectBoardSource(client, &ProjectBoardConfig{
				ProjectNumber: 99,
				StatusField:   "Status",
			}, "org", "repo")

			issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
			if err != nil {
				t.Fatalf("FindIssuesFromProject() error = %v", err)
			}
			if tc.wantInclude && len(issues) != 1 {
				t.Errorf("state=%q: expected issue to be included, got %d issues", tc.state, len(issues))
			}
			if !tc.wantInclude && len(issues) != 0 {
				t.Errorf("state=%q: expected issue to be excluded, got %d issues", tc.state, len(issues))
			}
		})
	}
}

func TestFindIssuesFromProject_DraftIssuesSkipped(t *testing.T) {
	server := fakeProjectSourceServer(t, []string{
		orgProjectResp("PVT_draft"),
		itemsResp([]map[string]interface{}{
			draftNode(),
			issueNode(30, "I_30", "Real issue", "", "OPEN", "org/repo", "Todo"),
		}, false, ""),
	})
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	src := NewProjectBoardSource(client, &ProjectBoardConfig{
		ProjectNumber: 5,
		StatusField:   "Status",
	}, "org", "repo")

	issues, err := src.FindIssuesFromProject(context.Background(), "Todo")
	if err != nil {
		t.Fatalf("FindIssuesFromProject() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 30 {
		t.Errorf("expected only issue #30, got %v", issues)
	}
}
