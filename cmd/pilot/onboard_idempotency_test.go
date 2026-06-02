package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestIdempotency tests that onboard detects already-configured states.
func TestIdempotency(t *testing.T) {
	tests := []struct {
		name              string
		setupConfig       func() *config.Config
		hasProjects       bool
		hasTickets        bool
		hasNotify         bool
		wantAllConfigured bool
	}{
		{
			name: "empty config",
			setupConfig: func() *config.Config {
				return config.DefaultConfig()
			},
			hasProjects:       false,
			hasTickets:        false,
			hasNotify:         false,
			wantAllConfigured: false,
		},
		{
			name: "has projects only",
			setupConfig: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Projects = []*config.ProjectConfig{
					{Name: "test-project", Path: "/path/to/project"},
				}
				return cfg
			},
			hasProjects:       true,
			hasTickets:        false,
			hasNotify:         false,
			wantAllConfigured: false,
		},
		{
			name: "has GitHub adapter configured",
			setupConfig: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Projects = []*config.ProjectConfig{
					{Name: "test-project", Path: "/path/to/project"},
				}
				cfg.Adapters = &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: true,
						Token:   testutil.FakeGitHubToken,
					},
				}
				return cfg
			},
			hasProjects:       true,
			hasTickets:        true,
			hasNotify:         false,
			wantAllConfigured: false,
		},
		{
			name: "fully configured - GitHub + Telegram",
			setupConfig: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Projects = []*config.ProjectConfig{
					{Name: "test-project", Path: "/path/to/project"},
				}
				cfg.Adapters = &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: true,
						Token:   testutil.FakeGitHubToken,
					},
					Telegram: &telegram.Config{
						Enabled:  true,
						BotToken: testutil.FakeTelegramBotToken,
					},
				}
				return cfg
			},
			hasProjects:       true,
			hasTickets:        true,
			hasNotify:         true,
			wantAllConfigured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setupConfig()

			// Test helper functions
			gotHasProjects := len(cfg.Projects) > 0
			gotHasTickets := hasTicketSource(cfg)
			gotHasNotify := hasNotificationChannel(cfg)

			if gotHasProjects != tt.hasProjects {
				t.Errorf("hasProjects = %v, want %v", gotHasProjects, tt.hasProjects)
			}
			if gotHasTickets != tt.hasTickets {
				t.Errorf("hasTicketSource() = %v, want %v", gotHasTickets, tt.hasTickets)
			}
			if gotHasNotify != tt.hasNotify {
				t.Errorf("hasNotificationChannel() = %v, want %v", gotHasNotify, tt.hasNotify)
			}

			// Check all configured state
			allConfigured := gotHasProjects && gotHasTickets && gotHasNotify
			if allConfigured != tt.wantAllConfigured {
				t.Errorf("allConfigured = %v, want %v", allConfigured, tt.wantAllConfigured)
			}
		})
	}
}

// TestHasTicketSource tests detection of configured ticket sources.
func TestHasTicketSource(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   bool
	}{
		{
			name:   "nil adapters",
			config: &config.Config{Adapters: nil},
			want:   false,
		},
		{
			name: "GitHub enabled",
			config: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{Enabled: true},
				},
			},
			want: true,
		},
		{
			name: "GitHub disabled",
			config: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{Enabled: false},
				},
			},
			want: false,
		},
		{
			name:   "empty adapters",
			config: &config.Config{Adapters: &config.AdaptersConfig{}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasTicketSource(tt.config); got != tt.want {
				t.Errorf("hasTicketSource() = %v, want %v", got, tt.want)
			}
		})
	}
}
