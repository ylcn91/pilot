package bitbucket

import (
	"testing"
	"time"
)

func TestExtractPRNumber(t *testing.T) {
	tests := []struct {
		name    string
		prURL   string
		want    int
		wantErr bool
	}{
		{
			name:    "standard Bitbucket PR URL",
			prURL:   "https://bitbucket.org/ws/repo/pull-requests/123",
			want:    123,
			wantErr: false,
		},
		{
			name:    "PR URL with trailing segment",
			prURL:   "https://bitbucket.org/ws/repo/pull-requests/456/diff",
			want:    456,
			wantErr: false,
		},
		{
			name:    "empty URL",
			prURL:   "",
			want:    0,
			wantErr: true,
		},
		{
			name:    "issue URL - no PR number",
			prURL:   "https://bitbucket.org/ws/repo/issues/123",
			want:    0,
			wantErr: true,
		},
		{
			name:    "random string",
			prURL:   "not-a-url",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractPRNumber(tt.prURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractPRNumber() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ExtractPRNumber() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNewPoller(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	label := "pilot"
	interval := 30 * time.Second

	poller := NewPoller(client, label, interval)
	if poller == nil {
		t.Fatal("NewPoller returned nil")
	}
	if poller.label != label {
		t.Errorf("poller.label = %s, want %s", poller.label, label)
	}
	if poller.interval != interval {
		t.Errorf("poller.interval = %v, want %v", poller.interval, interval)
	}
	if poller.executionMode != ExecutionModeParallel {
		t.Errorf("poller.executionMode = %v, want %v", poller.executionMode, ExecutionModeParallel)
	}
}

func TestNewPollerWithOptions(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")

	poller := NewPoller(client, "pilot", 30*time.Second,
		WithExecutionMode(ExecutionModeSequential),
		WithSequentialConfig(true, 30*time.Second, 1*time.Hour),
	)

	if poller.executionMode != ExecutionModeSequential {
		t.Errorf("poller.executionMode = %v, want %v", poller.executionMode, ExecutionModeSequential)
	}
	if !poller.waitForMerge {
		t.Error("poller.waitForMerge = false, want true")
	}
	if poller.prPollInterval != 30*time.Second {
		t.Errorf("poller.prPollInterval = %v, want 30s", poller.prPollInterval)
	}
	if poller.prTimeout != 1*time.Hour {
		t.Errorf("poller.prTimeout = %v, want 1h", poller.prTimeout)
	}
	if poller.mergeWaiter == nil {
		t.Error("poller.mergeWaiter is nil, expected non-nil for sequential mode")
	}
}

func TestPoller_IsProcessed(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	poller := NewPoller(client, "pilot", 30*time.Second)

	if poller.IsProcessed(42) {
		t.Error("expected issue 42 to not be processed initially")
	}

	poller.markProcessed(42)

	if !poller.IsProcessed(42) {
		t.Error("expected issue 42 to be processed after marking")
	}
	if poller.IsProcessed(43) {
		t.Error("expected issue 43 to not be processed")
	}
}

func TestPoller_ProcessedCount(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	poller := NewPoller(client, "pilot", 30*time.Second)

	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount() = %d, want 0", poller.ProcessedCount())
	}

	poller.markProcessed(1)
	poller.markProcessed(2)
	poller.markProcessed(3)

	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3", poller.ProcessedCount())
	}
}

func TestPoller_Reset(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	poller := NewPoller(client, "pilot", 30*time.Second)

	poller.markProcessed(1)
	poller.markProcessed(2)

	if poller.ProcessedCount() != 2 {
		t.Errorf("ProcessedCount() before reset = %d, want 2", poller.ProcessedCount())
	}

	poller.Reset()

	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount() after reset = %d, want 0", poller.ProcessedCount())
	}
	if poller.IsProcessed(1) {
		t.Error("expected issue 1 to not be processed after reset")
	}
}

func TestPoller_ClearProcessed(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	poller := NewPoller(client, "pilot", 30*time.Second)

	poller.markProcessed(5)
	poller.ClearProcessed(5)

	if poller.IsProcessed(5) {
		t.Error("expected issue 5 to be cleared")
	}
}

func TestPoller_MatchesLabel(t *testing.T) {
	client := NewClient(fakeToken, "ws", "repo")
	poller := NewPoller(client, "pilot", 30*time.Second)

	tests := []struct {
		name  string
		issue *Issue
		want  bool
	}{
		{
			name:  "kind matches pilot",
			issue: &Issue{ID: 1, Kind: "pilot"},
			want:  true,
		},
		{
			name:  "synthesized label matches pilot",
			issue: &Issue{ID: 2, Labels: []string{"pilot"}},
			want:  true,
		},
		{
			name:  "no match",
			issue: &Issue{ID: 3, Kind: "bug"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := poller.matchesLabel(tt.issue)
			if got != tt.want {
				t.Errorf("matchesLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExecutionModeConstants(t *testing.T) {
	if ExecutionModeSequential != "sequential" {
		t.Errorf("ExecutionModeSequential = %s, want 'sequential'", ExecutionModeSequential)
	}
	if ExecutionModeParallel != "parallel" {
		t.Errorf("ExecutionModeParallel = %s, want 'parallel'", ExecutionModeParallel)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("DefaultConfig().Enabled = true, want false")
	}
	if cfg.BaseURL != defaultBaseURL {
		t.Errorf("DefaultConfig().BaseURL = %s, want %s", cfg.BaseURL, defaultBaseURL)
	}
	if cfg.PilotLabel != "pilot" {
		t.Errorf("DefaultConfig().PilotLabel = %s, want pilot", cfg.PilotLabel)
	}
	if cfg.Polling == nil || cfg.Polling.Interval != 30*time.Second {
		t.Errorf("DefaultConfig().Polling.Interval unexpected: %+v", cfg.Polling)
	}
}
