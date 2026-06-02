package github

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewWebhookHandler(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	handler := NewWebhookHandler(client, "secret123", "pilot")

	if handler == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if handler.client != client {
		t.Error("handler.client not set correctly")
	}
	if handler.webhookSecret != "secret123" {
		t.Errorf("handler.webhookSecret = %s, want 'secret123'", handler.webhookSecret)
	}
	if handler.pilotLabel != "pilot" {
		t.Errorf("handler.pilotLabel = %s, want 'pilot'", handler.pilotLabel)
	}
}

func TestOnIssue(t *testing.T) {
	handler := NewWebhookHandler(nil, "", "pilot")

	handler.OnIssue(func(ctx context.Context, issue *Issue, repo *Repository) error {
		return nil
	})

	if handler.onIssue == nil {
		t.Error("OnIssue did not set callback")
	}
}

func TestHasPilotLabel(t *testing.T) {
	tests := []struct {
		name       string
		pilotLabel string
		labels     []Label
		want       bool
	}{
		{
			name:       "has pilot label",
			pilotLabel: "pilot",
			labels: []Label{
				{Name: "bug"},
				{Name: "pilot"},
			},
			want: true,
		},
		{
			name:       "no pilot label",
			pilotLabel: "pilot",
			labels: []Label{
				{Name: "bug"},
				{Name: "enhancement"},
			},
			want: false,
		},
		{
			name:       "empty labels",
			pilotLabel: "pilot",
			labels:     []Label{},
			want:       false,
		},
		{
			name:       "custom pilot label",
			pilotLabel: "ai-assist",
			labels: []Label{
				{Name: "ai-assist"},
			},
			want: true,
		},
		{
			name:       "case insensitive - matches",
			pilotLabel: "pilot",
			labels: []Label{
				{Name: "Pilot"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(nil, "", tt.pilotLabel)
			issue := &Issue{Labels: tt.labels}
			got := h.hasPilotLabel(issue)
			if got != tt.want {
				t.Errorf("hasPilotLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractIssueAndRepo(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
		wantErr bool
		check   func(t *testing.T, issue *Issue, repo *Repository)
	}{
		{
			name: "complete payload",
			payload: map[string]interface{}{
				"action": "labeled",
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Fix authentication bug",
					"body":     "The login form is broken",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels": []interface{}{
						map[string]interface{}{
							"id":   float64(123),
							"name": "bug",
						},
						map[string]interface{}{
							"id":   float64(456),
							"name": "pilot",
						},
					},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"clone_url": "https://github.com/org/repo.git",
					"ssh_url":   "git@github.com:org/repo.git",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, issue *Issue, repo *Repository) {
				if issue.Number != 42 {
					t.Errorf("issue.Number = %d, want 42", issue.Number)
				}
				if issue.Title != "Fix authentication bug" {
					t.Errorf("issue.Title = %s", issue.Title)
				}
				if issue.Body != "The login form is broken" {
					t.Errorf("issue.Body = %s", issue.Body)
				}
				if len(issue.Labels) != 2 {
					t.Errorf("len(issue.Labels) = %d, want 2", len(issue.Labels))
				}
				if repo.Owner.Login != "org" {
					t.Errorf("repo.Owner.Login = %s", repo.Owner.Login)
				}
				if repo.CloneURL != "https://github.com/org/repo.git" {
					t.Errorf("repo.CloneURL = %s", repo.CloneURL)
				}
				if repo.SSHURL != "git@github.com:org/repo.git" {
					t.Errorf("repo.SSHURL = %s", repo.SSHURL)
				}
			},
		},
		{
			name: "payload without body",
			payload: map[string]interface{}{
				"issue": map[string]interface{}{
					"number":   float64(42),
					"title":    "Issue without body",
					"state":    "open",
					"html_url": "https://github.com/org/repo/issues/42",
					"labels":   []interface{}{},
				},
				"repository": map[string]interface{}{
					"name":      "repo",
					"full_name": "org/repo",
					"html_url":  "https://github.com/org/repo",
					"owner": map[string]interface{}{
						"login": "org",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, issue *Issue, repo *Repository) {
				if issue.Body != "" {
					t.Errorf("issue.Body = %s, want empty", issue.Body)
				}
			},
		},
		{
			name:    "missing issue",
			payload: map[string]interface{}{"repository": map[string]interface{}{}},
			wantErr: true,
		},
		{
			name:    "missing repository",
			payload: map[string]interface{}{"issue": map[string]interface{}{}},
			wantErr: true,
		},
		{
			name:    "empty payload",
			payload: map[string]interface{}{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebhookHandler(nil, "", "pilot")
			issue, repo, err := h.extractIssueAndRepo(tt.payload)

			if (err != nil) != tt.wantErr {
				t.Errorf("extractIssueAndRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.check != nil {
				tt.check(t, issue, repo)
			}
		})
	}
}

func TestOnPRReview(t *testing.T) {
	handler := NewWebhookHandler(nil, "", "pilot")

	handler.OnPRReview(func(ctx context.Context, prNumber int, action, state, reviewer string, repo *Repository) error {
		return nil
	})

	if handler.onPRReview == nil {
		t.Error("OnPRReview did not set callback")
	}
}

func TestWebhookEventTypeConstants(t *testing.T) {
	if EventIssuesOpened != "issues.opened" {
		t.Errorf("EventIssuesOpened = %s, want 'issues.opened'", EventIssuesOpened)
	}
	if EventIssuesLabeled != "issues.labeled" {
		t.Errorf("EventIssuesLabeled = %s, want 'issues.labeled'", EventIssuesLabeled)
	}
	if EventIssuesClosed != "issues.closed" {
		t.Errorf("EventIssuesClosed = %s, want 'issues.closed'", EventIssuesClosed)
	}
	if EventIssueComment != "issue_comment.created" {
		t.Errorf("EventIssueComment = %s, want 'issue_comment.created'", EventIssueComment)
	}
	if EventPRReviewSubmitted != "pull_request_review.submitted" {
		t.Errorf("EventPRReviewSubmitted = %s, want 'pull_request_review.submitted'", EventPRReviewSubmitted)
	}
	if EventPRReviewDismissed != "pull_request_review.dismissed" {
		t.Errorf("EventPRReviewDismissed = %s, want 'pull_request_review.dismissed'", EventPRReviewDismissed)
	}
}
