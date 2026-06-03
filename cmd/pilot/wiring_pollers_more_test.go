package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/asana"
	"github.com/ylcn91/pilot/internal/adapters/azuredevops"
	"github.com/ylcn91/pilot/internal/adapters/discord"
	"github.com/ylcn91/pilot/internal/adapters/gitlab"
	"github.com/ylcn91/pilot/internal/adapters/jira"
	"github.com/ylcn91/pilot/internal/adapters/plane"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestPollerEnabled_Jira(t *testing.T) {
	reg := jiraPollerRegistration()
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "nil config", cfg: &config.Config{Adapters: &config.AdaptersConfig{}}, enabled: false},
		{name: "enabled without polling", cfg: &config.Config{Adapters: &config.AdaptersConfig{Jira: &jira.Config{Enabled: true}}}, enabled: false},
		{name: "polling disabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{Jira: &jira.Config{Enabled: true, Polling: &jira.PollingConfig{Enabled: false}}}}, enabled: false},
		{name: "fully enabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{Jira: &jira.Config{Enabled: true, BaseURL: "https://jira.test", APIToken: testutil.FakeJiraAPIToken, Polling: &jira.PollingConfig{Enabled: true}}}}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestPollerEnabled_Asana(t *testing.T) {
	reg := asanaPollerRegistration()
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "nil config", cfg: &config.Config{Adapters: &config.AdaptersConfig{}}, enabled: false},
		{name: "enabled without polling", cfg: &config.Config{Adapters: &config.AdaptersConfig{Asana: &asana.Config{Enabled: true}}}, enabled: false},
		{name: "fully enabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{Asana: &asana.Config{Enabled: true, AccessToken: testutil.FakeAsanaAccessToken, WorkspaceID: testutil.FakeAsanaWorkspaceID, Polling: &asana.PollingConfig{Enabled: true}}}}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestPollerEnabled_AzureDevOps(t *testing.T) {
	reg := azuredevopsPollerRegistration()
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "nil config", cfg: &config.Config{Adapters: &config.AdaptersConfig{}}, enabled: false},
		{name: "enabled without polling", cfg: &config.Config{Adapters: &config.AdaptersConfig{AzureDevOps: &azuredevops.Config{Enabled: true}}}, enabled: false},
		{name: "fully enabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{AzureDevOps: &azuredevops.Config{Enabled: true, PAT: testutil.FakeAzureDevOpsPAT, Organization: "test-org", Project: "test-project", Polling: &azuredevops.PollingConfig{Enabled: true}}}}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestPollerEnabled_Plane(t *testing.T) {
	reg := planePollerRegistration()
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "nil config", cfg: &config.Config{Adapters: &config.AdaptersConfig{}}, enabled: false},
		{name: "enabled without polling", cfg: &config.Config{Adapters: &config.AdaptersConfig{Plane: &plane.Config{Enabled: true}}}, enabled: false},
		{name: "fully enabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{Plane: &plane.Config{Enabled: true, BaseURL: "https://plane.test", APIKey: testutil.FakePlaneAPIKey, WorkspaceSlug: "test-ws", Polling: &plane.PollingConfig{Enabled: true}}}}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestPollerEnabled_Discord(t *testing.T) {
	reg := discordPollerRegistration()
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "nil config", cfg: &config.Config{Adapters: &config.AdaptersConfig{}}, enabled: false},
		{name: "adapter disabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{Discord: &discord.Config{Enabled: false}}}, enabled: false},
		{name: "enabled - no polling sub-config needed", cfg: &config.Config{Adapters: &config.AdaptersConfig{Discord: &discord.Config{Enabled: true, BotToken: testutil.FakeBearerToken}}}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestPollerEnabled_GitLab(t *testing.T) {
	reg := gitlabPollerRegistration()
	tests := []struct {
		name    string
		cfg     *config.Config
		enabled bool
	}{
		{name: "nil config", cfg: &config.Config{Adapters: &config.AdaptersConfig{}}, enabled: false},
		{name: "enabled without polling", cfg: &config.Config{Adapters: &config.AdaptersConfig{GitLab: &gitlab.Config{Enabled: true}}}, enabled: false},
		{name: "fully enabled", cfg: &config.Config{Adapters: &config.AdaptersConfig{GitLab: &gitlab.Config{Enabled: true, Token: testutil.FakeGitLabToken, Project: "group/project", Polling: &gitlab.PollingConfig{Enabled: true}}}}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg.Enabled(tt.cfg); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}
