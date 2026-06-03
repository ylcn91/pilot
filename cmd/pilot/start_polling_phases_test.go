package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/testutil"
)

// =============================================================================
// Gap (cli-config-wiring): pollingRuntime phase methods (setupStores,
// setupRunner) and validatePollingConfig had no unit tests; only the harness
// exercised a subset. These cover the testable, side-effect-bearing phases.
// =============================================================================

func TestValidatePollingConfig(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *config.Config
		wantErr        bool
		wantSocketMode bool // expected Slack.SocketMode after validation (when Slack set)
	}{
		{
			name: "telegram enabled without bot token errors",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Telegram: &telegram.Config{Enabled: true, BotToken: ""},
			}},
			wantErr: true,
		},
		{
			name: "telegram enabled with bot token ok",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Telegram: &telegram.Config{Enabled: true, BotToken: testutil.FakeTelegramBotToken},
			}},
			wantErr: false,
		},
		{
			name: "telegram disabled ok",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Telegram: &telegram.Config{Enabled: false},
			}},
			wantErr: false,
		},
		{
			name: "slack socket mode without app token degrades gracefully",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Telegram: &telegram.Config{Enabled: false},
				Slack:    &slack.Config{SocketMode: true, AppToken: ""},
			}},
			wantErr:        false,
			wantSocketMode: false,
		},
		{
			name: "slack socket mode with app token preserved",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Telegram: &telegram.Config{Enabled: false},
				Slack:    &slack.Config{SocketMode: true, AppToken: testutil.FakeSlackAppToken},
			}},
			wantErr:        false,
			wantSocketMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePollingConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePollingConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.cfg.Adapters.Slack != nil && tt.cfg.Adapters.Slack.SocketMode != tt.wantSocketMode {
				t.Errorf("Slack.SocketMode = %v, want %v", tt.cfg.Adapters.Slack.SocketMode, tt.wantSocketMode)
			}
		})
	}
}

func TestPollingRuntime_SetupRunner(t *testing.T) {
	tests := []struct {
		name    string
		quality *quality.Config
	}{
		{name: "no quality gates", quality: nil},
		{name: "quality gates enabled", quality: &quality.Config{Enabled: true, Gates: []*quality.Gate{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &pollingRuntime{
				cfg: &config.Config{
					Executor: executor.DefaultBackendConfig(),
					Quality:  tt.quality,
				},
			}
			if err := p.setupRunner(); err != nil {
				t.Fatalf("setupRunner() error = %v", err)
			}
			if p.runner == nil {
				t.Fatal("expected non-nil runner after setupRunner")
			}
		})
	}
}

func TestPollingRuntime_SetupStores(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Executor: executor.DefaultBackendConfig(),
		Memory: &config.MemoryConfig{
			Path: filepath.Join(tmpDir, "data"),
			// Disable the learning system so setupStores stays lightweight and
			// does not spin up the pattern store / 24h maintenance goroutine.
			Learning: &config.LearningConfig{Enabled: false},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &pollingRuntime{
		ctx:         ctx,
		cfg:         cfg,
		projectPath: tmpDir,
	}
	if err := p.setupRunner(); err != nil {
		t.Fatalf("setupRunner() error = %v", err)
	}

	cleanup := p.setupStores()
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup closure when store opens successfully")
	}
	defer cleanup()

	if p.store == nil {
		t.Fatal("expected store to be opened")
	}
	// GH-634: teams RBAC adapter wired when store available.
	if p.teamAdapter == nil {
		t.Error("expected teamAdapter to be wired after setupStores")
	}
	// GH-1027: knowledge store initialized when store available.
	if p.knowledgeStore == nil {
		t.Error("expected knowledgeStore to be initialized after setupStores")
	}
}
