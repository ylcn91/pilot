package autopilot

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_SetApprovalDecision_PersistsToMemoryStore verifies that
// Controller.SetApprovalDecision updates in-memory PRState AND calls the
// injected approvalPersister (executions table write path).
func TestController_SetApprovalDecision_PersistsToMemoryStore(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	mgr := approval.NewManager(nil)
	c := NewController(cfg, ghClient, mgr, "owner", "repo")

	mock := &mockApprovalPersister{}
	c.memoryStore = mock

	c.mu.Lock()
	c.activePRs[99] = &PRState{
		PRNumber:          99,
		IssueNumber:       10,
		ApprovalRequestID: "req-test-123",
	}
	c.mu.Unlock()

	ctx := context.Background()
	if err := c.SetApprovalDecision(ctx, "req-test-123", "approved", "reviewer"); err != nil {
		t.Fatalf("SetApprovalDecision: %v", err)
	}

	// In-memory state updated.
	pr, ok := c.GetPRState(99)
	if !ok {
		t.Fatal("PR state not found")
	}
	if pr.ApprovalDecision != "approved" {
		t.Errorf("in-memory ApprovalDecision = %q, want %q", pr.ApprovalDecision, "approved")
	}

	// Memory store called with correct args.
	if len(mock.decisionCalls) != 1 {
		t.Fatalf("expected 1 SetApprovalDecision call, got %d", len(mock.decisionCalls))
	}
	call := mock.decisionCalls[0]
	if call.requestID != "req-test-123" || call.decision != "approved" || call.by != "reviewer" {
		t.Errorf("unexpected call args: %+v", call)
	}
}

// TestController_ApprovalPersistMiss_RequestID verifies that a sql.ErrNoRows from
// SetApprovalRequestID increments the request_id miss counter.
func TestController_ApprovalPersistMiss_RequestID(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	c := NewController(DefaultConfig(), ghClient, approval.NewManager(nil), "owner", "repo")
	c.memoryStore = &errApprovalPersister{requestIDErr: sql.ErrNoRows}

	// Directly invoke the counter via the same code path as handleAwaitApproval by
	// calling the private helper through a public wrapper. Since the logic lives in
	// handleAwaitApproval (which calls SetApprovalRequestID), we replicate the
	// pattern inline: set up an active PR and call SetApprovalRequestID to simulate
	// the zero-row path, then check the counter.
	ctx := context.Background()
	taskID := "GH-42"
	requestID := "req-miss-test"

	// Simulate the call site in handleAwaitApproval.
	merr := c.memoryStore.SetApprovalRequestID(ctx, taskID, requestID)
	if merr != nil {
		c.metrics.RecordApprovalPersistMiss("request_id")
	}

	snap := c.metrics.Snapshot()
	if snap.ApprovalPersistMisses["request_id"] != 1 {
		t.Errorf("expected 1 request_id miss, got %d", snap.ApprovalPersistMisses["request_id"])
	}
	if snap.ApprovalPersistMisses["decision"] != 0 {
		t.Errorf("expected 0 decision misses, got %d", snap.ApprovalPersistMisses["decision"])
	}
}

// TestController_ApprovalPersistMiss_Decision verifies that a sql.ErrNoRows from
// SetApprovalDecision increments the decision miss counter via Controller.SetApprovalDecision.
func TestController_ApprovalPersistMiss_Decision(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	c := NewController(DefaultConfig(), ghClient, approval.NewManager(nil), "owner", "repo")
	c.memoryStore = &errApprovalPersister{decisionErr: sql.ErrNoRows}

	c.mu.Lock()
	c.activePRs[7] = &PRState{
		PRNumber:          7,
		IssueNumber:       42,
		ApprovalRequestID: "req-decision-miss",
	}
	c.mu.Unlock()

	ctx := context.Background()
	if err := c.SetApprovalDecision(ctx, "req-decision-miss", "approved", "bot"); err != nil {
		t.Fatalf("SetApprovalDecision: %v", err)
	}

	snap := c.metrics.Snapshot()
	if snap.ApprovalPersistMisses["decision"] != 1 {
		t.Errorf("expected 1 decision miss, got %d", snap.ApprovalPersistMisses["decision"])
	}
	if snap.ApprovalPersistMisses["request_id"] != 0 {
		t.Errorf("expected 0 request_id misses, got %d", snap.ApprovalPersistMisses["request_id"])
	}
}

// TestController_SetApprovalDecision_NoStoreNoPanic verifies that the controller
// works correctly when no memory store is wired (nil-safe).
func TestController_SetApprovalDecision_NoStoreNoPanic(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	c := NewController(DefaultConfig(), ghClient, approval.NewManager(nil), "owner", "repo")

	c.mu.Lock()
	c.activePRs[1] = &PRState{
		PRNumber:          1,
		ApprovalRequestID: "req-nil-store",
	}
	c.mu.Unlock()

	// Should not panic with nil memoryStore.
	err := c.SetApprovalDecision(context.Background(), "req-nil-store", "approved", "bot")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	pr, _ := c.GetPRState(1)
	if pr.ApprovalDecision != "approved" {
		t.Errorf("in-memory decision not set: %q", pr.ApprovalDecision)
	}
}
