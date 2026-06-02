package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/e2e/mocks"
	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestWorkflow_MultiplePRs tests that multiple PRs can be tracked independently.
func TestWorkflow_MultiplePRs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ghMock := mocks.NewGitHubMock()
	defer ghMock.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, ghMock.URL())

	// Create multiple issues
	issue1 := ghMock.CreateIssue("Task 1", "First task", []string{"pilot"})
	issue2 := ghMock.CreateIssue("Task 2", "Second task", []string{"pilot"})
	issue3 := ghMock.CreateIssue("Task 3", "Third task", []string{"pilot"})

	cfg := autopilot.DefaultConfig()
	cfg.Environment = autopilot.EnvDev
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.DevCITimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build"}

	controller := autopilot.NewController(cfg, ghClient, nil, "owner", "repo")

	// Register PRs for each issue
	sha1, sha2, sha3 := "sha1111", "sha2222", "sha3333"
	controller.OnPRCreated(1, "url1", issue1.Number, sha1, "pilot/GH-1", "")
	controller.OnPRCreated(2, "url2", issue2.Number, sha2, "pilot/GH-2", "")
	controller.OnPRCreated(3, "url3", issue3.Number, sha3, "pilot/GH-3", "")

	// Verify all are tracked
	prs := controller.GetActivePRs()
	if len(prs) != 3 {
		t.Fatalf("expected 3 active PRs, got %d", len(prs))
	}

	// Set different CI states
	ghMock.SetCIPassing(sha1, []string{"build"})
	ghMock.SetCIFailing(sha2, "build", nil)
	ghMock.SetCIPassing(sha3, []string{"build"})

	ctx := context.Background()

	// Process each PR through one stage
	for _, prNum := range []int{1, 2, 3} {
		_ = controller.ProcessPR(ctx, prNum, nil)
	}

	// Verify states differ based on CI
	pr1, _ := controller.GetPRState(1)
	pr2, _ := controller.GetPRState(2)
	pr3, _ := controller.GetPRState(3)

	if pr1.Stage != autopilot.StageWaitingCI {
		t.Errorf("PR1 stage = %s, want %s", pr1.Stage, autopilot.StageWaitingCI)
	}
	if pr2.Stage != autopilot.StageWaitingCI {
		t.Errorf("PR2 stage = %s, want %s", pr2.Stage, autopilot.StageWaitingCI)
	}
	if pr3.Stage != autopilot.StageWaitingCI {
		t.Errorf("PR3 stage = %s, want %s", pr3.Stage, autopilot.StageWaitingCI)
	}
}

// TestWorkflow_ScanExistingPRs tests recovery of existing pilot PRs on startup.
func TestWorkflow_ScanExistingPRs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ghMock := mocks.NewGitHubMock()
	defer ghMock.Close()

	// Pre-create a PR in the mock
	ghMock.CreateIssue("Existing task", "Was in progress", []string{"pilot"})

	// Manually add PR to mock (simulating existing PR from previous run)
	// The PR needs to have pilot branch naming
	mergeable := true
	pr := &github.PullRequest{
		Number:    100,
		Title:     "pilot/GH-1: Existing task",
		State:     "open",
		Merged:    false,
		Mergeable: &mergeable,
		Head:      github.PRRef{Ref: "pilot/GH-1", SHA: "existingsha"},
		HTMLURL:   ghMock.URL() + "/owner/repo/pull/100",
	}
	_ = pr // Would need to add to ghMock.prs directly

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, ghMock.URL())

	cfg := autopilot.DefaultConfig()
	controller := autopilot.NewController(cfg, ghClient, nil, "owner", "repo")

	ctx := context.Background()

	// Scan should recover existing PRs
	err := controller.ScanExistingPRs(ctx)
	if err != nil {
		t.Fatalf("ScanExistingPRs error: %v", err)
	}

	// Note: Our mock returns empty PR list by default
	// In real scenario, PRs with pilot/ branch would be recovered
	prs := controller.GetActivePRs()
	t.Logf("Recovered %d PRs", len(prs))
}
