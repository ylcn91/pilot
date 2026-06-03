package bitbucket

import (
	"testing"
)

func TestConvertIssueToTask(t *testing.T) {
	issue := &Issue{
		ID:       42,
		Title:    "Implement feature X",
		Content:  &Content{Raw: "This is the description\n\nWith multiple lines"},
		State:    StateNew,
		Kind:     "task",
		Priority: "major",
		Links:    &Links{HTML: &Link{Href: "https://bitbucket.org/ws/repo/issues/42"}},
	}
	issue.Labels = synthesizeLabels(issue)

	repo := &Repository{
		Name:     "repo",
		FullName: "ws/repo",
		Links:    &Links{HTML: &Link{Href: "https://bitbucket.org/ws/repo"}},
	}

	task := ConvertIssueToTask(issue, repo)

	if task.ID != "BB-42" {
		t.Errorf("task.ID = %s, want BB-42", task.ID)
	}
	if task.Title != "Implement feature X" {
		t.Errorf("task.Title = %s, want 'Implement feature X'", task.Title)
	}
	if task.IssueID != 42 {
		t.Errorf("task.IssueID = %d, want 42", task.IssueID)
	}
	if task.Workspace != "ws" {
		t.Errorf("task.Workspace = %s, want ws", task.Workspace)
	}
	if task.Repo != "repo" {
		t.Errorf("task.Repo = %s, want repo", task.Repo)
	}
	if task.IssueURL != "https://bitbucket.org/ws/repo/issues/42" {
		t.Errorf("task.IssueURL = %s, want issue html href", task.IssueURL)
	}
	if task.CloneURL != "https://bitbucket.org/ws/repo.git" {
		t.Errorf("task.CloneURL = %s, want repo.git", task.CloneURL)
	}
	if task.Priority != PriorityHigh {
		t.Errorf("task.Priority = %d, want %d (PriorityHigh)", task.Priority, PriorityHigh)
	}
}

func TestConvertIssueToTaskNilContent(t *testing.T) {
	issue := &Issue{ID: 1, Title: "No body"}
	task := ConvertIssueToTask(issue, nil)
	if task.Description != "" {
		t.Errorf("Description = %q, want empty", task.Description)
	}
	if task.IssueURL != "" {
		t.Errorf("IssueURL = %q, want empty", task.IssueURL)
	}
}

func TestSynthesizeLabels(t *testing.T) {
	issue := &Issue{Kind: "bug", Priority: "critical"}
	labels := synthesizeLabels(issue)
	if len(labels) != 2 {
		t.Fatalf("synthesizeLabels() = %v, want 2 entries", labels)
	}
	if labels[0] != "bug" {
		t.Errorf("labels[0] = %s, want bug", labels[0])
	}
	if labels[1] != "priority::critical" {
		t.Errorf("labels[1] = %s, want priority::critical", labels[1])
	}
}

func TestExtractDescription(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "simple description",
			body: "This is a simple description.",
			want: "This is a simple description.",
		},
		{
			name: "removes checklist section",
			body: "Main content\n\n### Checklist\n- [ ] Item 1\n- [ ] Item 2",
			want: "Main content",
		},
		{
			name: "removes environment section",
			body: "Main content\n\n### Environment\nOS: Linux",
			want: "Main content",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
		{
			name: "whitespace only",
			body: "   \n\n   ",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDescription(tt.body)
			if got != tt.want {
				t.Errorf("extractDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractPriority(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   Priority
	}{
		{name: "critical maps urgent", labels: []string{"priority::critical"}, want: PriorityUrgent},
		{name: "blocker maps urgent", labels: []string{"priority::blocker"}, want: PriorityUrgent},
		{name: "major maps high", labels: []string{"priority::major"}, want: PriorityHigh},
		{name: "scoped high", labels: []string{"priority::high"}, want: PriorityHigh},
		{name: "medium", labels: []string{"priority::medium"}, want: PriorityMedium},
		{name: "minor maps low", labels: []string{"priority::minor"}, want: PriorityLow},
		{name: "trivial maps low", labels: []string{"priority::trivial"}, want: PriorityLow},
		{name: "P0", labels: []string{"P0"}, want: PriorityUrgent},
		{name: "P1", labels: []string{"P1"}, want: PriorityHigh},
		{name: "P2", labels: []string{"P2"}, want: PriorityMedium},
		{name: "P3", labels: []string{"P3"}, want: PriorityLow},
		{name: "no priority", labels: []string{"bug"}, want: PriorityNone},
		{name: "empty", labels: []string{}, want: PriorityNone},
		{name: "nil", labels: nil, want: PriorityNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPriority(tt.labels)
			if got != tt.want {
				t.Errorf("extractPriority() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractLabelNames(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   []string
	}{
		{
			name:   "filters out pilot and priority labels",
			labels: []string{"pilot", "bug", "priority::high", "P1", "enhancement"},
			want:   []string{"bug", "enhancement"},
		},
		{
			name:   "keeps all non-filtered labels",
			labels: []string{"bug", "task", "enhancement"},
			want:   []string{"bug", "task", "enhancement"},
		},
		{
			name:   "all labels filtered",
			labels: []string{"pilot", "priority::high", "P0"},
			want:   nil,
		},
		{
			name:   "empty labels",
			labels: []string{},
			want:   nil,
		},
		{
			name:   "nil labels",
			labels: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLabelNames(tt.labels)

			if len(got) != len(tt.want) {
				t.Errorf("extractLabelNames() returned %d labels, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractLabelNames()[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPriorityFromLabelAndName(t *testing.T) {
	if PriorityFromLabel("P0") != PriorityUrgent {
		t.Error("PriorityFromLabel(P0) should be Urgent")
	}
	if PriorityName(PriorityHigh) != "High" {
		t.Errorf("PriorityName(High) = %s, want High", PriorityName(PriorityHigh))
	}
	if PriorityName(PriorityNone) != "No Priority" {
		t.Errorf("PriorityName(None) = %s, want No Priority", PriorityName(PriorityNone))
	}
}
