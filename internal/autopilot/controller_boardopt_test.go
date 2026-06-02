package autopilot

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestController_WithProjectBoardSync_AllStatuses verifies that WithProjectBoardSync
// stores all four status strings and wires the boardSync field.
func TestController_WithProjectBoardSync_AllStatuses(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	mock := &mockBoardSyncer{}
	opt := withBoardSyncerForTest(mock, "Done", "Failed", "In Review", "In Dev")

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
	if c.inProgressStatus != "In Dev" {
		t.Errorf("inProgressStatus = %q, want %q", c.inProgressStatus, "In Dev")
	}
}
