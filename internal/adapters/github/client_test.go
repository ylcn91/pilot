package github

import (
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewClient(t *testing.T) {
	client := NewClient(testutil.FakeGitHubToken)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.token != testutil.FakeGitHubToken {
		t.Errorf("client.token = %s, want %s", client.token, testutil.FakeGitHubToken)
	}
	if client.baseURL != githubAPIURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, githubAPIURL)
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	customURL := "https://custom.api.example.com"
	client := NewClientWithBaseURL(testutil.FakeGitHubToken, customURL)
	if client == nil {
		t.Fatal("NewClientWithBaseURL returned nil")
	}
	if client.token != testutil.FakeGitHubToken {
		t.Errorf("client.token = %s, want %s", client.token, testutil.FakeGitHubToken)
	}
	if client.baseURL != customURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, customURL)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled != false {
		t.Errorf("default Enabled = %v, want false", cfg.Enabled)
	}

	if cfg.PilotLabel != "pilot" {
		t.Errorf("default PilotLabel = %s, want 'pilot'", cfg.PilotLabel)
	}

	if cfg.Polling == nil {
		t.Fatal("default Polling config is nil")
	}

	if cfg.Polling.Enabled != false {
		t.Errorf("default Polling.Enabled = %v, want false", cfg.Polling.Enabled)
	}

	if cfg.Polling.Interval != 30*time.Second {
		t.Errorf("default Polling.Interval = %v, want 30s", cfg.Polling.Interval)
	}

	if cfg.Polling.Label != "pilot" {
		t.Errorf("default Polling.Label = %s, want 'pilot'", cfg.Polling.Label)
	}
}

func TestPriorityFromLabel(t *testing.T) {
	tests := []struct {
		label string
		want  Priority
	}{
		{"priority:urgent", PriorityUrgent},
		{"P0", PriorityUrgent},
		{"priority:high", PriorityHigh},
		{"P1", PriorityHigh},
		{"priority:medium", PriorityMedium},
		{"P2", PriorityMedium},
		{"priority:low", PriorityLow},
		{"P3", PriorityLow},
		{"bug", PriorityNone},
		{"", PriorityNone},
		{"random-label", PriorityNone},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := PriorityFromLabel(tt.label)
			if got != tt.want {
				t.Errorf("PriorityFromLabel(%s) = %d, want %d", tt.label, got, tt.want)
			}
		})
	}
}

func TestCommitStatusConstants(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{StatusPending, "pending"},
		{StatusSuccess, "success"},
		{StatusFailure, "failure"},
		{StatusError, "error"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}

func TestCheckRunConstants(t *testing.T) {
	statusTests := []struct {
		constant string
		expected string
	}{
		{CheckRunQueued, "queued"},
		{CheckRunInProgress, "in_progress"},
		{CheckRunCompleted, "completed"},
	}

	for _, tt := range statusTests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}

	conclusionTests := []struct {
		constant string
		expected string
	}{
		{ConclusionSuccess, "success"},
		{ConclusionFailure, "failure"},
		{ConclusionNeutral, "neutral"},
		{ConclusionCancelled, "cancelled"},
		{ConclusionTimedOut, "timed_out"},
		{ConclusionActionRequired, "action_required"},
		{ConclusionSkipped, "skipped"},
	}

	for _, tt := range conclusionTests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}

func TestStateConstants(t *testing.T) {
	if StateOpen != "open" {
		t.Errorf("StateOpen = %s, want 'open'", StateOpen)
	}
	if StateClosed != "closed" {
		t.Errorf("StateClosed = %s, want 'closed'", StateClosed)
	}
}

func TestLabelConstants(t *testing.T) {
	if LabelInProgress != "pilot-in-progress" {
		t.Errorf("LabelInProgress = %s, want 'pilot-in-progress'", LabelInProgress)
	}
	if LabelDone != "pilot-done" {
		t.Errorf("LabelDone = %s, want 'pilot-done'", LabelDone)
	}
	if LabelFailed != "pilot-failed" {
		t.Errorf("LabelFailed = %s, want 'pilot-failed'", LabelFailed)
	}
	// GH-1015: Added pilot-retry-ready for PRs closed without merge
	if LabelRetryReady != "pilot-retry-ready" {
		t.Errorf("LabelRetryReady = %s, want 'pilot-retry-ready'", LabelRetryReady)
	}
}

func TestMergeMethodConstants(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{MergeMethodMerge, "merge"},
		{MergeMethodSquash, "squash"},
		{MergeMethodRebase, "rebase"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}

func TestReviewEventConstants(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{ReviewEventApprove, "APPROVE"},
		{ReviewEventRequestChanges, "REQUEST_CHANGES"},
		{ReviewEventComment, "COMMENT"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}

func TestReviewStateConstants(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{ReviewStateApproved, "APPROVED"},
		{ReviewStateChangesRequested, "CHANGES_REQUESTED"},
		{ReviewStateCommented, "COMMENTED"},
		{ReviewStateDismissed, "DISMISSED"},
		{ReviewStatePending, "PENDING"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}
