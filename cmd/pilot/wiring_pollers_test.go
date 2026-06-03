package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/bitbucket"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/testutil"
)

// =============================================================================
// GH-2134: Per-adapter Enabled() wiring tests
//
// Each poller registration's Enabled() function must correctly gate on:
//   - adapter config being non-nil
//   - adapter .Enabled == true
//   - polling sub-config being non-nil and .Enabled == true (where applicable)
// =============================================================================

func TestPollerEnabled_Linear(t *testing.T) {
	reg := linearPollerRegistration()

	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{
			name:    "nil adapters",
			cfg:     &config.Config{Adapters: &config.AdaptersConfig{}},
			enabled: false,
		},
		{
			name: "adapter disabled",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Linear: &linear.Config{Enabled: false},
			}},
			enabled: false,
		},
		{
			name: "adapter enabled but no polling config",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Linear: &linear.Config{Enabled: true},
			}},
			enabled: false,
		},
		{
			name: "adapter enabled but polling disabled",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Linear: &linear.Config{
					Enabled: true,
					Polling: &linear.PollingConfig{Enabled: false},
				},
			}},
			enabled: false,
		},
		{
			name: "fully enabled",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Linear: &linear.Config{
					Enabled: true,
					APIKey:  testutil.FakeLinearAPIKey,
					Polling: &linear.PollingConfig{Enabled: true},
				},
			}},
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestPollerEnabled_Bitbucket(t *testing.T) {
	reg := bitbucketPollerRegistration()

	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{
			name:    "nil config",
			cfg:     &config.Config{Adapters: &config.AdaptersConfig{}},
			enabled: false,
		},
		{
			name: "enabled without polling",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Bitbucket: &bitbucket.Config{Enabled: true},
			}},
			enabled: false,
		},
		{
			name: "adapter enabled but polling disabled",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Bitbucket: &bitbucket.Config{
					Enabled: true,
					Polling: &bitbucket.PollingConfig{Enabled: false},
				},
			}},
			enabled: false,
		},
		{
			name: "fully enabled",
			cfg: &config.Config{Adapters: &config.AdaptersConfig{
				Bitbucket: &bitbucket.Config{
					Enabled:   true,
					Token:     testutil.FakeBitbucketToken,
					Workspace: "my-workspace",
					Repo:      "my-repo",
					Polling:   &bitbucket.PollingConfig{Enabled: true},
				},
			}},
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}
