package autopilot

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewController(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	approvalMgr := approval.NewManager(nil)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, approvalMgr, "owner", "repo")

	if c == nil {
		t.Fatal("NewController returned nil")
	}
	if c.owner != "owner" {
		t.Errorf("owner = %s, want owner", c.owner)
	}
	if c.repo != "repo" {
		t.Errorf("repo = %s, want repo", c.repo)
	}
	if c.ciMonitor == nil {
		t.Error("ciMonitor should be initialized")
	}
	if c.autoMerger == nil {
		t.Error("autoMerger should be initialized")
	}
	if c.feedbackLoop == nil {
		t.Error("feedbackLoop should be initialized")
	}
}

func TestNewController_ReleaserInit(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)

	enabledRelease := &ReleaseConfig{Enabled: true, Trigger: "on_merge"}
	disabledRelease := &ReleaseConfig{Enabled: false}

	tests := []struct {
		name          string
		globalRelease *ReleaseConfig
		envRelease    *ReleaseConfig
		wantReleaser  bool
	}{
		{
			name:          "global only enabled",
			globalRelease: enabledRelease,
			envRelease:    nil,
			wantReleaser:  true,
		},
		{
			name:          "env only enabled",
			globalRelease: nil,
			envRelease:    enabledRelease,
			wantReleaser:  true,
		},
		{
			name:          "both set — env wins (enabled)",
			globalRelease: disabledRelease,
			envRelease:    enabledRelease,
			wantReleaser:  true,
		},
		{
			name:          "neither set",
			globalRelease: nil,
			envRelease:    nil,
			wantReleaser:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Release = tt.globalRelease
			if tt.envRelease != nil {
				envCfg := &EnvironmentConfig{Release: tt.envRelease}
				cfg.activeEnvName = "test"
				cfg.activeEnvConfig = envCfg
			}

			c := NewController(cfg, ghClient, nil, "owner", "repo")

			if tt.wantReleaser && c.releaser == nil {
				t.Errorf("releaser should be non-nil")
			}
			if !tt.wantReleaser && c.releaser != nil {
				t.Errorf("releaser should be nil")
			}
		})
	}
}

func TestController_OnPRCreated(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	prs := c.GetActivePRs()
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}

	pr := prs[0]
	if pr.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", pr.PRNumber)
	}
	if pr.IssueNumber != 10 {
		t.Errorf("IssueNumber = %d, want 10", pr.IssueNumber)
	}
	if pr.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %s, want abc123", pr.HeadSHA)
	}
	if pr.Stage != StagePRCreated {
		t.Errorf("Stage = %s, want %s", pr.Stage, StagePRCreated)
	}
	if pr.CIStatus != CIPending {
		t.Errorf("CIStatus = %s, want %s", pr.CIStatus, CIPending)
	}
}

func TestController_GetPRState(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	pr, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("expected PR to be found")
	}
	if pr.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", pr.PRNumber)
	}

	_, ok = c.GetPRState(99)
	if ok {
		t.Error("PR 99 should not be found")
	}
}

func TestController_OnReviewRequested(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	// Should not panic on untracked PR
	c.OnReviewRequested(99, "submitted", "changes_requested", "reviewer1")

	// Register a PR and send review
	c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")
	c.OnReviewRequested(42, "submitted", "changes_requested", "reviewer1")

	// PR should still be tracked (stage transitions but PR remains in activePRs)
	pr, ok := c.GetPRState(42)
	if !ok {
		t.Fatal("expected PR to be tracked after review event")
	}
	if pr.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", pr.PRNumber)
	}

	// Approved review should also not panic
	c.OnReviewRequested(42, "submitted", "approved", "reviewer2")
}

func TestController_ProcessPR_NotTracked(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	err := c.ProcessPR(context.Background(), 99, nil)
	if err == nil {
		t.Error("ProcessPR should fail for untracked PR")
	}
}
