package health

import (
	"os"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/slack"
	"github.com/ylcn91/pilot/internal/adapters/telegram"
	"github.com/ylcn91/pilot/internal/config"
)

// ---------------------------------------------------------------------------
// checkConfig — table-driven tests with synthetic config
// ---------------------------------------------------------------------------

func TestCheckConfig_NoProjects(t *testing.T) {
	cfg := &config.Config{
		Projects: nil,
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "projects")
	if found == nil {
		t.Fatal("expected 'projects' check")
	}
	if found.Status != StatusWarning {
		t.Errorf("projects status = %v, want StatusWarning", found.Status)
	}
}

func TestCheckConfig_ValidProjects(t *testing.T) {
	// Use a path that exists on any system
	cfg := &config.Config{
		Projects: []*config.ProjectConfig{
			{Name: "test", Path: os.TempDir()},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "projects")
	if found == nil {
		t.Fatal("expected 'projects' check")
	}
	if found.Status != StatusOK {
		t.Errorf("projects status = %v, want StatusOK", found.Status)
	}
}

func TestCheckConfig_InvalidProjectPath(t *testing.T) {
	cfg := &config.Config{
		Projects: []*config.ProjectConfig{
			{Name: "test", Path: "/nonexistent/path/xyz123"},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "projects")
	if found == nil {
		t.Fatal("expected 'projects' check")
	}
	if found.Status != StatusWarning {
		t.Errorf("projects status = %v, want StatusWarning", found.Status)
	}
}

// GH-2361: warn when projects are configured but no issue-source adapter is enabled.
func TestCheckConfig_Adapters_ProjectsButNoAdapter(t *testing.T) {
	cfg := &config.Config{
		Projects: []*config.ProjectConfig{
			{Name: "myrepo", Path: os.TempDir()},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "adapters")
	if found == nil {
		t.Fatal("expected 'adapters' check")
	}
	if found.Status != StatusWarning {
		t.Errorf("adapters status = %v, want StatusWarning", found.Status)
	}
	if !strings.Contains(found.Message, "no issue source") {
		t.Errorf("adapters message = %q, want mention of 'no issue source'", found.Message)
	}
}

func TestCheckConfig_Adapters_GitHubEnabled(t *testing.T) {
	cfg := &config.Config{
		Projects: []*config.ProjectConfig{
			{Name: "myrepo", Path: os.TempDir()},
		},
		Adapters: &config.AdaptersConfig{
			GitHub: &github.Config{Enabled: true, Token: "test-gh-token", Repo: "org/repo"},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "adapters")
	if found == nil {
		t.Fatal("expected 'adapters' check")
	}
	if found.Status != StatusOK {
		t.Errorf("adapters status = %v, want StatusOK", found.Status)
	}
}

func TestCheckConfig_Adapters_GitHubPresentButDisabled(t *testing.T) {
	cfg := &config.Config{
		Projects: []*config.ProjectConfig{
			{Name: "myrepo", Path: os.TempDir()},
		},
		Adapters: &config.AdaptersConfig{
			GitHub: &github.Config{Enabled: false},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "adapters")
	if found == nil {
		t.Fatal("expected 'adapters' check")
	}
	if found.Status != StatusWarning {
		t.Errorf("adapters status = %v, want StatusWarning", found.Status)
	}
}

func TestCheckConfig_Adapters_NoProjectsSkipped(t *testing.T) {
	cfg := &config.Config{}
	checks := checkConfig(cfg)

	if found := findConfigCheck(checks, "adapters"); found != nil {
		t.Errorf("adapters check should not appear when no projects configured, got %+v", found)
	}
}

func TestCheckConfig_TelegramEnabled_NoToken(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{
				Enabled:  true,
				BotToken: "",
			},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "telegram.bot_token")
	if found == nil {
		t.Fatal("expected 'telegram.bot_token' check")
	}
	if found.Status != StatusError {
		t.Errorf("telegram.bot_token status = %v, want StatusError", found.Status)
	}
}

func TestCheckConfig_TelegramEnabled_WithToken(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Telegram: &telegram.Config{
				Enabled:  true,
				BotToken: "test-bot-token",
			},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "telegram.bot_token")
	if found == nil {
		t.Fatal("expected 'telegram.bot_token' check")
	}
	if found.Status != StatusOK {
		t.Errorf("telegram.bot_token status = %v, want StatusOK", found.Status)
	}
}

func TestCheckConfig_SlackEnabled_NoToken(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Slack: &slack.Config{
				Enabled:  true,
				BotToken: "",
			},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "slack.bot_token")
	if found == nil {
		t.Fatal("expected 'slack.bot_token' check")
	}
	if found.Status != StatusError {
		t.Errorf("slack.bot_token status = %v, want StatusError", found.Status)
	}
}

func TestCheckConfig_SlackEnabled_WithToken(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			Slack: &slack.Config{
				Enabled:  true,
				BotToken: "test-slack-bot-token",
			},
		},
	}
	checks := checkConfig(cfg)

	found := findConfigCheck(checks, "slack.bot_token")
	if found == nil {
		t.Fatal("expected 'slack.bot_token' check")
	}
	if found.Status != StatusOK {
		t.Errorf("slack.bot_token status = %v, want StatusOK", found.Status)
	}
}

func TestCheckConfig_GitHubChecks(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *config.Config
		wantCheckName string
		wantStatus    Status
	}{
		{
			name: "enabled no token no gh auth",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: true,
						Token:   "",
					},
				},
			},
			wantCheckName: ghAuthFallbackCheckName(t),
			wantStatus:    ghAuthFallbackStatus(t),
		},
		{
			name: "enabled no repos polling on",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: true,
						Token:   "test-gh-token",
						Polling: &github.PollingConfig{Enabled: true},
					},
				},
			},
			wantCheckName: "github.repos",
			wantStatus:    StatusWarning,
		},
		{
			name: "fully configured",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: true,
						Token:   "test-gh-token",
						Repo:    "org/repo",
					},
				},
			},
			wantCheckName: "github",
			wantStatus:    StatusOK,
		},
		{
			name: "disabled entirely",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: false,
					},
				},
			},
			wantCheckName: "", // no github check expected
			wantStatus:    StatusDisabled,
		},
		{
			name: "token with project-level repos",
			cfg: &config.Config{
				Adapters: &config.AdaptersConfig{
					GitHub: &github.Config{
						Enabled: true,
						Token:   "test-gh-token",
						Polling: &github.PollingConfig{Enabled: true},
					},
				},
				Projects: []*config.ProjectConfig{
					{
						Name: "myproj",
						Path: "/tmp",
						GitHub: &config.ProjectGitHubConfig{
							Owner: "org",
							Repo:  "repo",
						},
					},
				},
			},
			wantCheckName: "github",
			wantStatus:    StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := checkConfig(tt.cfg)
			if tt.wantCheckName == "" {
				// No GitHub check should appear
				for _, c := range checks {
					if strings.HasPrefix(c.Name, "github") {
						t.Errorf("found unexpected github check %q (status=%v), want none", c.Name, c.Status)
					}
				}
				return
			}
			found := findConfigCheck(checks, tt.wantCheckName)
			if found == nil {
				t.Fatalf("expected check %q, not found in %v", tt.wantCheckName, checkNames(checks))
			}
			if found.Status != tt.wantStatus {
				t.Errorf("%q status = %v, want %v", tt.wantCheckName, found.Status, tt.wantStatus)
			}
		})
	}
}
