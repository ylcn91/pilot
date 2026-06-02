package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestAutoMerger_MergePR_SquashUsePRTitle(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		prTitle   string
		wantTitle string
	}{
		{
			name:      "squash with PR title uses PR title",
			method:    github.MergeMethodSquash,
			prTitle:   "feat(api): add rate limiting",
			wantTitle: "feat(api): add rate limiting",
		},
		{
			name:      "squash without PR title falls back to default",
			method:    github.MergeMethodSquash,
			prTitle:   "",
			wantTitle: "Merge PR #42",
		},
		{
			name:      "regular merge ignores PR title",
			method:    github.MergeMethodMerge,
			prTitle:   "feat(api): add rate limiting",
			wantTitle: "Merge PR #42",
		},
		{
			name:      "rebase ignores PR title",
			method:    github.MergeMethodRebase,
			prTitle:   "fix(auth): token refresh",
			wantTitle: "Merge PR #42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedTitle := ""

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls/42/merge" {
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					capturedTitle = body["commit_title"]
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.Environment = EnvDev
			cfg.AutoReview = false
			cfg.MergeMethod = tt.method

			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

			err := merger.MergePR(context.Background(), &PRState{
				PRNumber: 42,
				PRTitle:  tt.prTitle,
			})
			if err != nil {
				t.Errorf("MergePR() error = %v", err)
			}

			if capturedTitle != tt.wantTitle {
				t.Errorf("commit_title = %q, want %q", capturedTitle, tt.wantTitle)
			}
		})
	}
}

func TestAutoMerger_MergePR_SquashStripsIssuePrefix(t *testing.T) {
	tests := []struct {
		name        string
		prTitle     string
		issueNumber int
		wantTitle   string
	}{
		{
			name:        "strips GH-XXXX: prefix from squash commit",
			prTitle:     "GH-2312: fix(autopilot): strip issue prefix from squash title",
			issueNumber: 2312,
			wantTitle:   "fix(autopilot): strip issue prefix from squash title",
		},
		{
			name:        "no strip when issue number mismatches",
			prTitle:     "GH-9999: feat(scope): something",
			issueNumber: 2312,
			wantTitle:   "GH-9999: feat(scope): something",
		},
		{
			name:        "no strip when no issue number",
			prTitle:     "GH-2312: fix(autopilot): something",
			issueNumber: 0,
			wantTitle:   "GH-2312: fix(autopilot): something",
		},
		{
			name:        "plain conventional commit unchanged",
			prTitle:     "feat(api): add endpoint",
			issueNumber: 2312,
			wantTitle:   "feat(api): add endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedTitle := ""

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls/42/merge" {
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					capturedTitle = body["commit_title"]
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.Environment = EnvDev
			cfg.AutoReview = false
			cfg.MergeMethod = github.MergeMethodSquash

			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

			err := merger.MergePR(context.Background(), &PRState{
				PRNumber:    42,
				PRTitle:     tt.prTitle,
				IssueNumber: tt.issueNumber,
			})
			if err != nil {
				t.Errorf("MergePR() error = %v", err)
			}

			if capturedTitle != tt.wantTitle {
				t.Errorf("commit_title = %q, want %q", capturedTitle, tt.wantTitle)
			}
		})
	}
}
