package main

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/config"
)

// =============================================================================
// GH-635: applyTeamOverrides tests
// =============================================================================

func TestApplyTeamOverrides_NotChanged(t *testing.T) {
	cfg := &config.Config{}
	cmd := &cobra.Command{}
	cmd.Flags().String("team", "", "")
	cmd.Flags().String("team-member", "", "")

	applyTeamOverrides(cfg, cmd, "my-team", "dev@test.com")

	if cfg.Team != nil {
		t.Error("expected Team to remain nil when --team flag not changed")
	}
}

func TestApplyTeamOverrides_TeamFlagSet(t *testing.T) {
	cfg := &config.Config{}
	cmd := &cobra.Command{}
	cmd.Flags().String("team", "", "")
	cmd.Flags().String("team-member", "", "")
	_ = cmd.Flags().Set("team", "my-team")

	applyTeamOverrides(cfg, cmd, "my-team", "")

	if cfg.Team == nil {
		t.Fatal("expected Team to be created")
	}
	if !cfg.Team.Enabled {
		t.Error("expected Team.Enabled to be true")
	}
	if cfg.Team.TeamID != "my-team" {
		t.Errorf("expected TeamID 'my-team', got %q", cfg.Team.TeamID)
	}
}

func TestApplyTeamOverrides_BothFlagsSet(t *testing.T) {
	cfg := &config.Config{}
	cmd := &cobra.Command{}
	cmd.Flags().String("team", "", "")
	cmd.Flags().String("team-member", "", "")
	_ = cmd.Flags().Set("team", "my-team")
	_ = cmd.Flags().Set("team-member", "dev@test.com")

	applyTeamOverrides(cfg, cmd, "my-team", "dev@test.com")

	if cfg.Team == nil {
		t.Fatal("expected Team to be created")
	}
	if cfg.Team.TeamID != "my-team" {
		t.Errorf("expected TeamID 'my-team', got %q", cfg.Team.TeamID)
	}
	if cfg.Team.MemberEmail != "dev@test.com" {
		t.Errorf("expected MemberEmail 'dev@test.com', got %q", cfg.Team.MemberEmail)
	}
}

func TestApplyTeamOverrides_OverridesExistingConfig(t *testing.T) {
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     false,
			TeamID:      "old-team",
			MemberEmail: "old@test.com",
		},
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("team", "", "")
	cmd.Flags().String("team-member", "", "")
	_ = cmd.Flags().Set("team", "new-team")

	applyTeamOverrides(cfg, cmd, "new-team", "")

	if !cfg.Team.Enabled {
		t.Error("expected Team.Enabled to be true after override")
	}
	if cfg.Team.TeamID != "new-team" {
		t.Errorf("expected TeamID 'new-team', got %q", cfg.Team.TeamID)
	}
	// MemberEmail should be preserved from existing config when --team-member not set
	if cfg.Team.MemberEmail != "old@test.com" {
		t.Errorf("expected MemberEmail preserved as 'old@test.com', got %q", cfg.Team.MemberEmail)
	}
}

// =============================================================================
// GH-711: applyInputOverrides tests
// =============================================================================

func TestApplyInputOverrides(t *testing.T) {
	tests := []struct {
		name          string
		setFlags      map[string]string // flags to mark as "changed"
		telegram      bool
		github        bool
		linear        bool
		slack         bool
		tunnel        bool
		checkTelegram *bool // expected Telegram.Enabled (nil = skip check)
		checkGitHub   *bool
		checkLinear   *bool
		checkSlack    *bool
		checkSocket   *bool // expected Slack.SocketMode
		checkTunnel   *bool
	}{
		{
			name:     "no flags changed — config untouched",
			setFlags: map[string]string{},
		},
		{
			name:        "slack flag enables slack and socket mode",
			setFlags:    map[string]string{"slack": "true"},
			slack:       true,
			checkSlack:  boolPtr(true),
			checkSocket: boolPtr(true),
		},
		{
			name:        "slack flag false disables slack and socket mode",
			setFlags:    map[string]string{"slack": "false"},
			slack:       false,
			checkSlack:  boolPtr(false),
			checkSocket: boolPtr(false),
		},
		{
			name:          "telegram flag enables telegram",
			setFlags:      map[string]string{"telegram": "true"},
			telegram:      true,
			checkTelegram: boolPtr(true),
		},
		{
			name:        "github flag enables github polling",
			setFlags:    map[string]string{"github": "true"},
			github:      true,
			checkGitHub: boolPtr(true),
		},
		{
			name:        "linear flag enables linear",
			setFlags:    map[string]string{"linear": "true"},
			linear:      true,
			checkLinear: boolPtr(true),
		},
		{
			name:        "tunnel flag enables tunnel",
			setFlags:    map[string]string{"tunnel": "true"},
			tunnel:      true,
			checkTunnel: boolPtr(true),
		},
		{
			name:     "multiple flags at once",
			setFlags: map[string]string{"slack": "true", "telegram": "true", "github": "true"},
			slack:    true, telegram: true, github: true,
			checkSlack:    boolPtr(true),
			checkSocket:   boolPtr(true),
			checkTelegram: boolPtr(true),
			checkGitHub:   boolPtr(true),
		},
		{
			name:        "slack on nil config creates default",
			setFlags:    map[string]string{"slack": "true"},
			slack:       true,
			checkSlack:  boolPtr(true),
			checkSocket: boolPtr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Adapters: &config.AdaptersConfig{}}
			cmd := &cobra.Command{}
			// Register all flags so Changed() works
			cmd.Flags().Bool("telegram", false, "")
			cmd.Flags().Bool("github", false, "")
			cmd.Flags().Bool("linear", false, "")
			cmd.Flags().Bool("slack", false, "")
			cmd.Flags().Bool("tunnel", false, "")

			for k, v := range tt.setFlags {
				_ = cmd.Flags().Set(k, v)
			}

			applyInputOverrides(cfg, cmd, tt.telegram, tt.github, tt.linear, tt.slack, tt.tunnel, false, false)

			if tt.checkTelegram != nil {
				if cfg.Adapters.Telegram == nil {
					t.Fatal("expected Telegram config to be created")
				}
				if cfg.Adapters.Telegram.Enabled != *tt.checkTelegram {
					t.Errorf("Telegram.Enabled = %v, want %v", cfg.Adapters.Telegram.Enabled, *tt.checkTelegram)
				}
			}
			if tt.checkGitHub != nil {
				if cfg.Adapters.GitHub == nil {
					t.Fatal("expected GitHub config to be created")
				}
				if cfg.Adapters.GitHub.Enabled != *tt.checkGitHub {
					t.Errorf("GitHub.Enabled = %v, want %v", cfg.Adapters.GitHub.Enabled, *tt.checkGitHub)
				}
			}
			if tt.checkLinear != nil {
				if cfg.Adapters.Linear == nil {
					t.Fatal("expected Linear config to be created")
				}
				if cfg.Adapters.Linear.Enabled != *tt.checkLinear {
					t.Errorf("Linear.Enabled = %v, want %v", cfg.Adapters.Linear.Enabled, *tt.checkLinear)
				}
			}
			if tt.checkSlack != nil {
				if cfg.Adapters.Slack == nil {
					t.Fatal("expected Slack config to be created")
				}
				if cfg.Adapters.Slack.Enabled != *tt.checkSlack {
					t.Errorf("Slack.Enabled = %v, want %v", cfg.Adapters.Slack.Enabled, *tt.checkSlack)
				}
			}
			if tt.checkSocket != nil {
				if cfg.Adapters.Slack == nil {
					t.Fatal("expected Slack config to be created for SocketMode check")
				}
				if cfg.Adapters.Slack.SocketMode != *tt.checkSocket {
					t.Errorf("Slack.SocketMode = %v, want %v", cfg.Adapters.Slack.SocketMode, *tt.checkSocket)
				}
			}
			if tt.checkTunnel != nil {
				if cfg.Tunnel == nil {
					t.Fatal("expected Tunnel config to be created")
				}
				if cfg.Tunnel.Enabled != *tt.checkTunnel {
					t.Errorf("Tunnel.Enabled = %v, want %v", cfg.Tunnel.Enabled, *tt.checkTunnel)
				}
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestCountGitHubRepos(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want int
	}{
		{
			name: "nil github adapter",
			cfg:  &config.Config{},
			want: 0,
		},
		{
			name: "default repo only",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{Repo: "owner/repo"},
				},
			},
			want: 1,
		},
		{
			name: "project repos only",
			cfg: &config.Config{
				Projects: []*config.ProjectConfig{
					{Name: "a", GitHub: &config.ProjectGitHubConfig{Owner: "org", Repo: "proj-a"}},
					{Name: "b", GitHub: &config.ProjectGitHubConfig{Owner: "org", Repo: "proj-b"}},
				},
			},
			want: 2,
		},
		{
			name: "default plus projects deduped",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{Repo: "org/proj-a"},
				},
				Projects: []*config.ProjectConfig{
					{Name: "a", GitHub: &config.ProjectGitHubConfig{Owner: "org", Repo: "proj-a"}},
					{Name: "b", GitHub: &config.ProjectGitHubConfig{Owner: "org", Repo: "proj-b"}},
				},
			},
			want: 2, // org/proj-a deduplicated
		},
		{
			name: "empty strings ignored",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{Repo: ""},
				},
				Projects: []*config.ProjectConfig{
					{Name: "a", GitHub: &config.ProjectGitHubConfig{Owner: "org", Repo: ""}},
					{Name: "b", GitHub: &config.ProjectGitHubConfig{Owner: "", Repo: "repo"}},
				},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countGitHubRepos(tt.cfg)
			if got != tt.want {
				t.Errorf("countGitHubRepos() = %d, want %d", got, tt.want)
			}
		})
	}
}
