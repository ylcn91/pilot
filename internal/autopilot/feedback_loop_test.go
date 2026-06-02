package autopilot

import (
	"regexp"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewFeedbackLoop(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	cfg.IssueLabels = []string{"pilot", "autopilot-fix"}

	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	if fl == nil {
		t.Fatal("NewFeedbackLoop returned nil")
	}
	if fl.owner != "owner" {
		t.Errorf("owner = %s, want owner", fl.owner)
	}
	if fl.repo != "repo" {
		t.Errorf("repo = %s, want repo", fl.repo)
	}
	if len(fl.issueLabels) != 2 {
		t.Errorf("issueLabels = %v, want 2 labels", fl.issueLabels)
	}
}

func TestFeedbackLoop_GenerateTitle(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	tests := []struct {
		name        string
		failureType FailureType
		prNumber    int
		wantTitle   string
	}{
		{
			name:        "CI pre-merge",
			failureType: FailureCIPreMerge,
			prNumber:    42,
			wantTitle:   "fix(ci): resolve CI failure from PR #42",
		},
		{
			name:        "CI post-merge",
			failureType: FailureCIPostMerge,
			prNumber:    123,
			wantTitle:   "fix(ci): resolve post-merge CI failure from PR #123",
		},
		{
			name:        "merge conflict",
			failureType: FailureMerge,
			prNumber:    99,
			wantTitle:   "fix(merge): resolve merge conflict for PR #99",
		},
		{
			name:        "deployment",
			failureType: FailureDeployment,
			prNumber:    1,
			wantTitle:   "fix(deploy): resolve deployment failure from PR #1",
		},
		{
			name:        "unknown type",
			failureType: FailureType("unknown"),
			prNumber:    50,
			wantTitle:   "fix(autopilot): resolve issue from PR #50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prState := &PRState{PRNumber: tt.prNumber}
			got := fl.generateTitle(prState, tt.failureType)
			if got != tt.wantTitle {
				t.Errorf("generateTitle() = %q, want %q", got, tt.wantTitle)
			}
		})
	}
}

// TestFeedbackLoop_GenerateTitle_ConventionalCommits verifies every failure type
// produces a title matching the conventional-commits pattern so Pilot's title
// validator never blocks autopilot-generated issues.
func TestFeedbackLoop_GenerateTitle_ConventionalCommits(t *testing.T) {
	conventionalRe := regexp.MustCompile(`^(feat|fix|chore|refactor|test|docs)(\([^)]+\))?: `)

	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	failureTypes := []FailureType{
		FailureCIPreMerge,
		FailureCIPostMerge,
		FailureMerge,
		FailureDeployment,
		FailureReviewRequested,
		FailureType("unknown"),
	}

	for _, ft := range failureTypes {
		t.Run(string(ft), func(t *testing.T) {
			prState := &PRState{PRNumber: 42}
			title := fl.generateTitle(prState, ft)
			if !conventionalRe.MatchString(title) {
				t.Errorf("title %q does not match conventional-commits pattern", title)
			}
		})
	}
}

func TestFeedbackLoop_SetLearningLoop(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.AutoCreateIssues = true
	fl := NewFeedbackLoop(ghClient, "owner", "repo", cfg)

	if fl.learningLoop != nil {
		t.Error("learningLoop should be nil initially")
	}

	// SetLearningLoop accepts nil gracefully
	fl.SetLearningLoop(nil)
	if fl.learningLoop != nil {
		t.Error("learningLoop should remain nil when set to nil")
	}
}

func TestFailureTypes(t *testing.T) {
	// Verify all failure type constants
	tests := []struct {
		ft   FailureType
		want string
	}{
		{FailureCIPreMerge, "ci_pre_merge"},
		{FailureCIPostMerge, "ci_post_merge"},
		{FailureMerge, "merge_conflict"},
		{FailureDeployment, "deployment"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.ft) != tt.want {
				t.Errorf("FailureType = %s, want %s", tt.ft, tt.want)
			}
		})
	}
}
