package autopilot

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_WithProjectBoardSync_AllStatuses verifies that WithProjectBoardSync
// stores the controller-owned status strings and wires the boardSync field. The
// In-Progress transition is owned by the poller, not the controller (#15).
func TestController_WithProjectBoardSync_AllStatuses(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	mock := &mockBoardSyncer{}
	opt := withBoardSyncerForTest(mock, "Done", "Failed", "In Review")

	c := NewController(DefaultConfig(), ghClient, nil, "owner", "repo", opt)

	if c.boardSync == nil {
		t.Fatal("boardSync should be set")
	}
	if c.doneStatus != "Done" {
		t.Errorf("doneStatus = %q, want %q", c.doneStatus, "Done")
	}
	if c.failStatus != "Failed" {
		t.Errorf("failStatus = %q, want %q", c.failStatus, "Failed")
	}
	if c.reviewStatus != "In Review" {
		t.Errorf("reviewStatus = %q, want %q", c.reviewStatus, "In Review")
	}
}
