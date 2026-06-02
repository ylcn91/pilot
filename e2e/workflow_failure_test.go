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

// TestWorkflow_CIFailure tests the CI failure → fix issue creation path.
func TestWorkflow_CIFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ghMock := mocks.NewGitHubMock()
	defer ghMock.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, ghMock.URL())

	issue := ghMock.CreateIssue(
		"Feature with failing CI",
		"This will fail CI",
		[]string{"pilot"},
	)

	cfg := autopilot.DefaultConfig()
	cfg.Environment = autopilot.EnvStage
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build", "test", "lint"}

	controller := autopilot.NewController(cfg, ghClient, nil, "owner", "repo")

	prSHA := "failsha123"
	branchName := "pilot/GH-" + itoa(issue.Number)

	// Set CI to fail
	ghMock.SetCIFailing(prSHA, "build", []string{"test", "lint"})

	controller.OnPRCreated(1, ghMock.URL()+"/owner/repo/pull/1", issue.Number, prSHA, branchName, "")

	ctx := context.Background()

	// Stage 1: PR created → waiting CI
	_ = controller.ProcessPR(ctx, 1, nil)

	// Stage 2: waiting CI → CI failed
	_ = controller.ProcessPR(ctx, 1, nil)
	prState, _ := controller.GetPRState(1)
	if prState.Stage != autopilot.StageCIFailed {
		t.Errorf("stage 2: got %s, want %s", prState.Stage, autopilot.StageCIFailed)
	}
	if prState.CIStatus != autopilot.CIFailure {
		t.Errorf("CIStatus = %s, want %s", prState.CIStatus, autopilot.CIFailure)
	}

	// Stage 3: CI failed → creates fix issue → failed state
	_ = controller.ProcessPR(ctx, 1, nil)
	prState, _ = controller.GetPRState(1)
	if prState.Stage != autopilot.StageFailed {
		t.Errorf("stage 3: got %s, want %s", prState.Stage, autopilot.StageFailed)
	}
}

// TestWorkflow_CircuitBreaker tests that repeated failures trip the circuit breaker.
func TestWorkflow_CircuitBreaker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ghMock := mocks.NewGitHubMock()
	defer ghMock.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, ghMock.URL())

	issue := ghMock.CreateIssue("Problematic task", "Will fail repeatedly", []string{"pilot"})

	cfg := autopilot.DefaultConfig()
	cfg.Environment = autopilot.EnvDev
	cfg.MaxFailures = 3

	controller := autopilot.NewController(cfg, ghClient, nil, "owner", "repo")

	// Start PR in merging stage (will fail to merge due to server error)
	controller.OnPRCreated(1, "url", issue.Number, "sha123", "pilot/GH-1", "")

	// Manually set to merging stage
	prState, _ := controller.GetPRState(1)
	prState.Stage = autopilot.StageMerging

	ctx := context.Background()

	// The mock server will return 404 for merge (PR doesn't exist in mock's PR map)
	// This causes repeated failures
	for i := 0; i < 4; i++ {
		_ = controller.ProcessPR(ctx, 1, nil)
	}

	// Circuit breaker should be open
	if !controller.IsPRCircuitOpen(1) {
		t.Error("circuit breaker should be open after repeated failures")
	}

	// Further processing should fail
	err := controller.ProcessPR(ctx, 1, nil)
	if err == nil {
		t.Error("ProcessPR should fail when circuit is open")
	}

	// Reset and verify processing resumes
	controller.ResetPRCircuitBreaker(1)
	if controller.IsPRCircuitOpen(1) {
		t.Error("circuit should be closed after reset")
	}
}
