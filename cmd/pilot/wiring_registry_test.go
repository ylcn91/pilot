package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/discord"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/testutil"
)

// =============================================================================
// GH-2134: Verify all adapter registrations have correct names
// =============================================================================

func TestPollerRegistrationNames(t *testing.T) {
	regs := adapterPollerRegistrations()

	names := make(map[string]bool)
	for _, reg := range regs {
		if reg.Name == "" {
			t.Error("found registration with empty name")
		}
		if names[reg.Name] {
			t.Errorf("duplicate registration name: %q", reg.Name)
		}
		names[reg.Name] = true

		if reg.Enabled == nil {
			t.Errorf("registration %q has nil Enabled func", reg.Name)
		}
		if reg.CreateAndStart == nil {
			t.Errorf("registration %q has nil CreateAndStart func", reg.Name)
		}
	}
}

// =============================================================================
// GH-2134: Verify multiple adapters can be enabled simultaneously
// =============================================================================

func TestPollerEnabled_MultipleAdaptersSimultaneously(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Linear: &linear.Config{
				Enabled: true,
				APIKey:  testutil.FakeLinearAPIKey,
				Polling: &linear.PollingConfig{Enabled: true},
			},
			Jira: &jira.Config{
				Enabled:  true,
				BaseURL:  "https://jira.test",
				APIToken: testutil.FakeJiraAPIToken,
				Polling:  &jira.PollingConfig{Enabled: true},
			},
			Discord: &discord.Config{
				Enabled:  true,
				BotToken: testutil.FakeBearerToken,
			},
			GitLab: &gitlab.Config{
				Enabled: true,
				Token:   testutil.FakeGitLabToken,
				Polling: &gitlab.PollingConfig{Enabled: true},
			},
			// These remain disabled
			Asana:       &asana.Config{Enabled: false},
			AzureDevOps: &azuredevops.Config{Enabled: false},
			Plane:       &plane.Config{Enabled: false},
		},
	}

	regs := adapterPollerRegistrations()

	expectedEnabled := map[string]bool{
		"linear":      true,
		"jira":        true,
		"discord":     true,
		"gitlab":      true,
		"asana":       false,
		"azuredevops": false,
		"plane":       false,
	}

	for _, reg := range regs {
		want, ok := expectedEnabled[reg.Name]
		if !ok {
			t.Errorf("unexpected registration name %q", reg.Name)
			continue
		}
		if got := reg.Enabled(cfg); got != want {
			t.Errorf("adapter %q: Enabled() = %v, want %v", reg.Name, got, want)
		}
	}
}
