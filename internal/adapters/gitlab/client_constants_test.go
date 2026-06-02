package gitlab

import (
	"testing"
)

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name      string
		issue     *Issue
		labelName string
		want      bool
	}{
		{
			name: "has label - first",
			issue: &Issue{
				Labels: []string{"pilot", "bug"},
			},
			labelName: "pilot",
			want:      true,
		},
		{
			name: "has label - last",
			issue: &Issue{
				Labels: []string{"bug", "enhancement", "pilot"},
			},
			labelName: "pilot",
			want:      true,
		},
		{
			name: "does not have label",
			issue: &Issue{
				Labels: []string{"bug", "enhancement"},
			},
			labelName: "pilot",
			want:      false,
		},
		{
			name: "empty labels",
			issue: &Issue{
				Labels: []string{},
			},
			labelName: "pilot",
			want:      false,
		},
		{
			name:      "nil labels",
			issue:     &Issue{},
			labelName: "pilot",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasLabel(tt.issue, tt.labelName)
			if got != tt.want {
				t.Errorf("HasLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled != false {
		t.Errorf("default Enabled = %v, want false", cfg.Enabled)
	}

	if cfg.BaseURL != "https://gitlab.com" {
		t.Errorf("default BaseURL = %s, want 'https://gitlab.com'", cfg.BaseURL)
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

	if cfg.Polling.Label != "pilot" {
		t.Errorf("default Polling.Label = %s, want 'pilot'", cfg.Polling.Label)
	}
}

func TestPriorityFromLabel(t *testing.T) {
	tests := []struct {
		label string
		want  Priority
	}{
		{"priority::urgent", PriorityUrgent},
		{"P0", PriorityUrgent},
		{"priority::high", PriorityHigh},
		{"P1", PriorityHigh},
		{"priority::medium", PriorityMedium},
		{"P2", PriorityMedium},
		{"priority::low", PriorityLow},
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

func TestStateConstants(t *testing.T) {
	if StateOpened != "opened" {
		t.Errorf("StateOpened = %s, want 'opened'", StateOpened)
	}
	if StateClosed != "closed" {
		t.Errorf("StateClosed = %s, want 'closed'", StateClosed)
	}
}

func TestMRStateConstants(t *testing.T) {
	if MRStateOpened != "opened" {
		t.Errorf("MRStateOpened = %s, want 'opened'", MRStateOpened)
	}
	if MRStateClosed != "closed" {
		t.Errorf("MRStateClosed = %s, want 'closed'", MRStateClosed)
	}
	if MRStateMerged != "merged" {
		t.Errorf("MRStateMerged = %s, want 'merged'", MRStateMerged)
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
}

func TestPipelineStatusConstants(t *testing.T) {
	tests := []struct {
		constant string
		expected string
	}{
		{PipelinePending, "pending"},
		{PipelineRunning, "running"},
		{PipelineSuccess, "success"},
		{PipelineFailed, "failed"},
		{PipelineCanceled, "canceled"},
		{PipelineSkipped, "skipped"},
		{PipelineManual, "manual"},
	}

	for _, tt := range tests {
		if tt.constant != tt.expected {
			t.Errorf("constant = %s, want %s", tt.constant, tt.expected)
		}
	}
}
