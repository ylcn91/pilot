package main

import (
	"testing"
	"time"

	githubadapter "github.com/ylcn91/pilot/internal/adapters/github"
	telegramadapter "github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
)

func tgCfg(allowedIDs []int64, chatID string) *config.Config {
	return &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegramadapter.Config{
				AllowedIDs: allowedIDs,
				ChatID:     chatID,
			},
		},
	}
}

func TestResolveTelegramAllowedIDs(t *testing.T) {
	tests := []struct {
		name       string
		allowedIDs []int64
		chatID     string
		want       []int64
	}{
		{
			name:       "empty",
			allowedIDs: nil,
			chatID:     "",
			want:       nil,
		},
		{
			name:       "only AllowedIDs",
			allowedIDs: []int64{1, 2, 3},
			chatID:     "",
			want:       []int64{1, 2, 3},
		},
		{
			name:       "only ChatID",
			allowedIDs: nil,
			chatID:     "42",
			want:       []int64{42},
		},
		{
			name:       "both",
			allowedIDs: []int64{1, 2},
			chatID:     "99",
			want:       []int64{1, 2, 99},
		},
		{
			name:       "unparseable ChatID",
			allowedIDs: []int64{7},
			chatID:     "not-a-number",
			want:       []int64{7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTelegramAllowedIDs(tgCfg(tt.allowedIDs, tt.chatID))
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d] = %d, want %d (got %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestResolveGitHubExecutionParams(t *testing.T) {
	tests := []struct {
		name             string
		execution        *config.ExecutionConfig
		wantExecMode     githubadapter.ExecutionMode
		wantWaitForMerge bool
		wantPollInterval time.Duration
		wantPRTimeout    time.Duration
		wantModeStr      string
	}{
		{
			name:             "defaults (nil execution)",
			execution:        nil,
			wantExecMode:     githubadapter.ExecutionModeSequential,
			wantWaitForMerge: true,
			wantPollInterval: 30 * time.Second,
			wantPRTimeout:    1 * time.Hour,
			wantModeStr:      "sequential",
		},
		{
			name: "parallel mode",
			execution: &config.ExecutionConfig{
				Mode:         "parallel",
				WaitForMerge: false,
			},
			wantExecMode:     githubadapter.ExecutionModeParallel,
			wantWaitForMerge: false,
			wantPollInterval: 30 * time.Second,
			wantPRTimeout:    1 * time.Hour,
			wantModeStr:      "parallel",
		},
		{
			name: "custom poll/timeout overrides",
			execution: &config.ExecutionConfig{
				Mode:         "sequential",
				WaitForMerge: true,
				PollInterval: 10 * time.Second,
				PRTimeout:    2 * time.Hour,
			},
			wantExecMode:     githubadapter.ExecutionModeSequential,
			wantWaitForMerge: true,
			wantPollInterval: 10 * time.Second,
			wantPRTimeout:    2 * time.Hour,
			wantModeStr:      "sequential",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Orchestrator: &config.OrchestratorConfig{
					Execution: tt.execution,
				},
			}
			execMode, waitForMerge, pollInterval, prTimeout, modeStr := resolveGitHubExecutionParams(cfg)
			if execMode != tt.wantExecMode {
				t.Errorf("execMode = %v, want %v", execMode, tt.wantExecMode)
			}
			if waitForMerge != tt.wantWaitForMerge {
				t.Errorf("waitForMerge = %v, want %v", waitForMerge, tt.wantWaitForMerge)
			}
			if pollInterval != tt.wantPollInterval {
				t.Errorf("pollInterval = %v, want %v", pollInterval, tt.wantPollInterval)
			}
			if prTimeout != tt.wantPRTimeout {
				t.Errorf("prTimeout = %v, want %v", prTimeout, tt.wantPRTimeout)
			}
			if modeStr != tt.wantModeStr {
				t.Errorf("modeStr = %q, want %q", modeStr, tt.wantModeStr)
			}
		})
	}
}
