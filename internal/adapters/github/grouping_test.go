package github

import (
	"testing"
	"time"
)

func TestParseParentIssueNumber(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "GH-style parent reference",
			body: "Parent: GH-150\n\nImplement the widget",
			want: 150,
		},
		{
			name: "hash-style parent reference",
			body: "Parent: #42\n\nDo the thing",
			want: 42,
		},
		{
			name: "no parent reference",
			body: "Just a regular issue body with no parent metadata",
			want: 0,
		},
		{
			name: "empty body",
			body: "",
			want: 0,
		},
		{
			name: "parent reference mid-body",
			body: "Some description\nParent: GH-99\nMore text",
			want: 99,
		},
		{
			name: "parent in prose does not match",
			body: "This is the parent of all issues. Parent GH-5 is referenced.",
			want: 0,
		},
		{
			name: "parent reference with extra whitespace",
			body: "Parent:  GH-200\n\nDescription",
			want: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseParentIssueNumber(tt.body)
			if got != tt.want {
				t.Errorf("ParseParentIssueNumber() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGroupIssues_EmptyInput(t *testing.T) {
	result := GroupIssues(nil)
	if result != nil {
		t.Errorf("GroupIssues(nil) = %v, want nil", result)
	}

	result = GroupIssues([]*Issue{})
	if result != nil {
		t.Errorf("GroupIssues([]) = %v, want nil", result)
	}
}

func TestGroupIssues_StandalonePassthrough(t *testing.T) {
	now := time.Now()
	issues := []*Issue{
		{Number: 10, Title: "Standalone A", State: "open", CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 20, Title: "Standalone B", State: "open", CreatedAt: now.Add(-1 * time.Hour)},
	}

	result := GroupIssues(issues)
	if len(result) != 2 {
		t.Fatalf("expected 2 grouped issues, got %d", len(result))
	}

	for _, g := range result {
		if g.IsEpic {
			t.Errorf("standalone issue %d should not be epic", g.Issue.Number)
		}
		if g.TotalSubs != 0 {
			t.Errorf("standalone issue %d should have 0 TotalSubs, got %d", g.Issue.Number, g.TotalSubs)
		}
		if g.DoneSubs != 0 {
			t.Errorf("standalone issue %d should have 0 DoneSubs, got %d", g.Issue.Number, g.DoneSubs)
		}
		if !g.IsActive {
			t.Errorf("open standalone issue %d should be active", g.Issue.Number)
		}
	}
}

func TestGroupIssues_EpicAbsorbsChildren(t *testing.T) {
	now := time.Now()
	parent := &Issue{
		Number:    100,
		Title:     "Epic: Build auth system",
		State:     "open",
		CreatedAt: now.Add(-3 * time.Hour),
	}
	child1 := &Issue{
		Number:    101,
		Title:     "Add login endpoint",
		Body:      "Parent: GH-100\n\nImplement login",
		State:     "closed",
		Labels:    []Label{{Name: LabelDone}},
		CreatedAt: now.Add(-2 * time.Hour),
	}
	child2 := &Issue{
		Number:    102,
		Title:     "Add signup endpoint",
		Body:      "Parent: GH-100\n\nImplement signup",
		State:     "open",
		CreatedAt: now.Add(-1 * time.Hour),
	}
	child3 := &Issue{
		Number:    103,
		Title:     "Add password reset",
		Body:      "Parent: GH-100\n\nImplement reset",
		State:     "closed",
		CreatedAt: now,
	}

	issues := []*Issue{parent, child1, child2, child3}
	result := GroupIssues(issues)

	// Children should be absorbed — only parent appears at top level.
	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue (the epic), got %d", len(result))
	}

	epic := result[0]
	if !epic.IsEpic {
		t.Error("expected IsEpic=true")
	}
	if epic.Issue.Number != 100 {
		t.Errorf("expected epic issue number 100, got %d", epic.Issue.Number)
	}
	if epic.TotalSubs != 3 {
		t.Errorf("expected TotalSubs=3, got %d", epic.TotalSubs)
	}
	if epic.DoneSubs != 2 {
		t.Errorf("expected DoneSubs=2 (child1 closed+done label, child3 closed), got %d", epic.DoneSubs)
	}
	if !epic.IsActive {
		t.Error("expected epic to be active (1 sub-issue still open)")
	}
	if len(epic.SubIssues) != 3 {
		t.Errorf("expected 3 sub-issues, got %d", len(epic.SubIssues))
	}

	// Verify sub-issues sorted by creation date.
	for i := 1; i < len(epic.SubIssues); i++ {
		if epic.SubIssues[i].CreatedAt.Before(epic.SubIssues[i-1].CreatedAt) {
			t.Errorf("sub-issues not sorted by creation date at index %d", i)
		}
	}
}

func TestGroupIssues_CompletedEpic(t *testing.T) {
	now := time.Now()
	parent := &Issue{
		Number:    200,
		Title:     "Epic: Refactor DB layer",
		State:     "open",
		CreatedAt: now.Add(-5 * time.Hour),
	}
	child1 := &Issue{
		Number:    201,
		Title:     "Migrate to pgx",
		Body:      "Parent: GH-200\n\nSwitch driver",
		State:     "closed",
		Labels:    []Label{{Name: LabelDone}},
		CreatedAt: now.Add(-4 * time.Hour),
	}
	child2 := &Issue{
		Number:    202,
		Title:     "Update queries",
		Body:      "Parent: GH-200\n\nRewrite SQL",
		State:     "closed",
		CreatedAt: now.Add(-3 * time.Hour),
	}

	issues := []*Issue{parent, child1, child2}
	result := GroupIssues(issues)

	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue, got %d", len(result))
	}

	epic := result[0]
	if !epic.IsEpic {
		t.Error("expected IsEpic=true")
	}
	if epic.DoneSubs != 2 {
		t.Errorf("expected DoneSubs=2, got %d", epic.DoneSubs)
	}
	if epic.TotalSubs != 2 {
		t.Errorf("expected TotalSubs=2, got %d", epic.TotalSubs)
	}
	if epic.IsActive {
		t.Error("expected epic to be completed (all subs done)")
	}
}
