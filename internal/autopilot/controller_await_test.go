package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_AwaitApproval_StaysInStageUntilDecision verifies that the
// non-blocking handleAwaitApproval submits a request on the first tick and
// stays in StageAwaitApproval until a decision is recorded.
func TestController_AwaitApproval_StaysInStageUntilDecision(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.Environment = EnvProd

	mgr := asyncApprovalManager()
	c := NewController(cfg, ghClient, mgr, "owner", "repo")

	// Plant a PR directly in StageAwaitApproval.
	c.mu.Lock()
	c.activePRs[42] = &PRState{
		PRNumber:    42,
		PRURL:       "https://github.com/owner/repo/pull/42",
		PRTitle:     "feat: something",
		IssueNumber: 10,
		Stage:       StageAwaitApproval,
	}
	c.mu.Unlock()

	ctx := context.Background()

	// Tick 1: no ApprovalRequestID yet — should submit and stay in stage.
	if err := c.ProcessPR(ctx, 42, nil); err != nil {
		t.Fatalf("tick 1 error: %v", err)
	}
	pr, _ := c.GetPRState(42)
	if pr.Stage != StageAwaitApproval {
		t.Errorf("after tick 1: stage = %s, want %s", pr.Stage, StageAwaitApproval)
	}
	if pr.ApprovalRequestID == "" {
		t.Error("after tick 1: ApprovalRequestID must be set")
	}
	if pr.ApprovalRequestedAt.IsZero() {
		t.Error("after tick 1: ApprovalRequestedAt must be set")
	}

	// Tick 2: request submitted, no decision yet — should stay in stage.
	if err := c.ProcessPR(ctx, 42, nil); err != nil {
		t.Fatalf("tick 2 error: %v", err)
	}
	pr, _ = c.GetPRState(42)
	if pr.Stage != StageAwaitApproval {
		t.Errorf("after tick 2: stage = %s, want %s", pr.Stage, StageAwaitApproval)
	}

	// Record approval decision directly (simulating a Telegram callback).
	_ = c.SetApprovalDecision(ctx, pr.ApprovalRequestID, string(approval.DecisionApproved), "testuser")

	// Tick 3: decision recorded — should advance to StageMerging.
	if err := c.ProcessPR(ctx, 42, nil); err != nil {
		t.Fatalf("tick 3 error: %v", err)
	}
	pr, _ = c.GetPRState(42)
	if pr.Stage != StageMerging {
		t.Errorf("after approval tick: stage = %s, want %s", pr.Stage, StageMerging)
	}
}

// TestController_AwaitApproval_AppliesDefaultActionAtTimeout verifies that when
// ApprovalRequestedAt exceeds the stage timeout and no decision is recorded, the
// controller applies the configured default_action.
func TestController_AwaitApproval_AppliesDefaultActionAtTimeout(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	cfg.Environment = EnvProd

	// Short timeout so the test can simulate expiry without sleeping.
	approvalCfg := &approval.Config{
		Enabled: true,

		DefaultTimeout: 1 * time.Millisecond,
		DefaultAction:  approval.DecisionRejected,
		PreMerge: &approval.StageConfig{
			Enabled:       true,
			Timeout:       1 * time.Millisecond,
			DefaultAction: approval.DecisionRejected,
		},
	}
	mgr := approval.NewManager(approvalCfg)
	c := NewController(cfg, ghClient, mgr, "owner", "repo")

	// Plant a PR in StageAwaitApproval with a request already submitted but expired.
	c.mu.Lock()
	c.activePRs[42] = &PRState{
		PRNumber:            42,
		PRURL:               "https://github.com/owner/repo/pull/42",
		IssueNumber:         10,
		Stage:               StageAwaitApproval,
		ApprovalRequestID:   "req-expired",
		ApprovalRequestedAt: time.Now().Add(-2 * time.Hour), // well past timeout
	}
	c.mu.Unlock()

	ctx := context.Background()

	// Single tick: should detect expiry, apply default_action (rejected), set StageFailed.
	if err := c.ProcessPR(ctx, 42, nil); err != nil {
		t.Fatalf("tick error: %v", err)
	}
	pr, _ := c.GetPRState(42)
	if pr.Stage != StageFailed {
		t.Errorf("after timeout: stage = %s, want %s", pr.Stage, StageFailed)
	}
	if pr.ApprovalDecision != string(approval.DecisionRejected) {
		t.Errorf("after timeout: decision = %q, want %q", pr.ApprovalDecision, approval.DecisionRejected)
	}
}

// TestController_TwoPRQueue_StalledApprovalDoesNotBlockOther verifies that a PR
// stalled in StageAwaitApproval does not prevent another PR from advancing through
// earlier stages.
func TestController_TwoPRQueue_StalledApprovalDoesNotBlockOther(t *testing.T) {
	// Server returns success for any check-run or merge request.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "check-runs"):
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "success"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	// Use stage env so PR-B can reach StageMerging without blocking on approval.
	cfg.Environment = EnvStage
	cfg.CIPollInterval = 10 * time.Millisecond
	cfg.CIWaitTimeout = 1 * time.Second
	cfg.RequiredChecks = []string{"build"}

	mgr := asyncApprovalManager()
	c := NewController(cfg, ghClient, mgr, "owner", "repo")

	ctx := context.Background()

	// PR-A: stalled in StageAwaitApproval with request already submitted.
	c.mu.Lock()
	c.activePRs[10] = &PRState{
		PRNumber:            10,
		PRURL:               "https://github.com/owner/repo/pull/10",
		IssueNumber:         1,
		HeadSHA:             "sha10",
		Stage:               StageAwaitApproval,
		ApprovalRequestID:   "req-stalled",
		ApprovalRequestedAt: time.Now(),
	}
	c.mu.Unlock()

	// PR-B: freshly created, will advance independently.
	c.OnPRCreated(20, "https://github.com/owner/repo/pull/20", 2, "sha20", "pilot/GH-2", "")

	// Tick both PRs — PR-A stays stalled, PR-B advances from StagePRCreated → StageWaitingCI.
	_ = c.ProcessPR(ctx, 10, nil)
	_ = c.ProcessPR(ctx, 20, nil)

	prA, _ := c.GetPRState(10)
	prB, _ := c.GetPRState(20)

	if prA.Stage != StageAwaitApproval {
		t.Errorf("PR-A should still be %s, got %s", StageAwaitApproval, prA.Stage)
	}
	if prB.Stage == StagePRCreated {
		t.Errorf("PR-B should have advanced past %s (was blocked by PR-A)", StagePRCreated)
	}
}
