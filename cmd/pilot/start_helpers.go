package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
)

// cleanStartupHooks removes stale pilot hooks from .claude/settings.json
// for the active project and all explicitly configured projects.
func cleanStartupHooks(cfg *config.Config, projectPath string) {
	seen := make(map[string]bool)

	// Clean the resolved projectPath
	if projectPath != "" {
		seen[projectPath] = true
		settingsPath := filepath.Join(projectPath, ".claude", "settings.json")
		if err := executor.CleanStalePilotHooks(settingsPath); err != nil {
			slog.Warn("failed to clean stale hooks", "path", projectPath, "error", err)
		}
	}

	// Clean all explicitly configured projects
	for _, p := range cfg.Projects {
		if p.Path == "" || seen[p.Path] {
			continue
		}
		seen[p.Path] = true
		settingsPath := filepath.Join(p.Path, ".claude", "settings.json")
		if err := executor.CleanStalePilotHooks(settingsPath); err != nil {
			slog.Warn("failed to clean stale hooks", "path", p.Path, "error", err)
		}
	}
}

// countGitHubRepos counts unique GitHub repos from the default config and project-level entries.
// applyDashboardBannerMeta populates the dashboard banner with env name,
// model stack (plan/exec), session code, and a per-adapter active/configured
// list so the banner reflects what's actually running this session.
// (GH-2459 — rework of the wiring shipped in GH-2455.)
//
// An adapter contributes a chip to the banner when it is configured (non-nil
// + Enabled) in cfg. Active=true when the corresponding CLI flag was passed
// on this invocation; Active=false renders an empty circle.
func applyDashboardBannerMeta(model *dashboard.Model, cfg *config.Config, cmd *cobra.Command) {
	envName := ""
	if cfg.Orchestrator != nil && cfg.Orchestrator.Autopilot != nil {
		envName = string(cfg.Orchestrator.Autopilot.Environment)
	}

	modelStack := ""
	if cfg.Executor != nil {
		def := shortenModelID(cfg.Executor.DefaultModel)
		var complex string
		if cfg.Executor.ModelRouting != nil {
			complex = shortenModelID(cfg.Executor.ModelRouting.Complex)
		}
		switch {
		case complex != "" && def != "" && complex != def:
			modelStack = complex + " / " + def
		case def != "":
			modelStack = def
		case complex != "":
			modelStack = complex
		}
	}

	flagPassed := func(name string) bool {
		if cmd == nil {
			return true // no cobra context — assume runtime active for back-compat
		}
		f := cmd.Flags().Lookup(name)
		if f == nil {
			return false
		}
		return f.Changed
	}

	var adapters []dashboard.AdapterStatus
	if cfg.Adapters != nil {
		if cfg.Adapters.GitHub != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "GH",
				Active: cfg.Adapters.GitHub.Enabled && flagPassed("github"),
			})
		}
		if cfg.Adapters.Telegram != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "TG",
				Active: cfg.Adapters.Telegram.Enabled && flagPassed("telegram"),
			})
		}
		if cfg.Adapters.Slack != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "SLACK",
				Active: cfg.Adapters.Slack.Enabled && flagPassed("slack"),
			})
		}
		if cfg.Adapters.Discord != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "DISCORD",
				Active: cfg.Adapters.Discord.Enabled && flagPassed("discord"),
			})
		}
		if cfg.Adapters.Linear != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "LINEAR",
				Active: cfg.Adapters.Linear.Enabled && flagPassed("linear"),
			})
		}
		if cfg.Adapters.Jira != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "JIRA",
				Active: cfg.Adapters.Jira.Enabled,
			})
		}
		if cfg.Adapters.GitLab != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "GL",
				Active: cfg.Adapters.GitLab.Enabled,
			})
		}
		if cfg.Adapters.Plane != nil {
			adapters = append(adapters, dashboard.AdapterStatus{
				Name:   "PLANE",
				Active: cfg.Adapters.Plane.Enabled && flagPassed("plane"),
			})
		}
	}

	model.SetBannerMeta(envName, modelStack, nil, time.Now())
	model.SetBannerAdapters(adapters)
}

// resolvedConfigPath returns the user-facing path to ~/.pilot/config.yaml
// (with $HOME contracted to ~) for display in the splash boot block.
func resolvedConfigPath() string {
	home, _ := os.UserHomeDir()
	full := filepath.Join(home, ".pilot", "config.yaml")
	if home != "" && strings.HasPrefix(full, home) {
		return "~" + strings.TrimPrefix(full, home)
	}
	return full
}

// shortenModelID compacts a model identifier for the banner: strips the
// vendor prefix ("claude-", "gpt-", etc.) and uppercases the rest so
// "claude-opus-4-7" → "OPUS-4-7". Returns empty string for empty input.
func shortenModelID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, prefix := range []string{"claude-", "gpt-", "anthropic-", "openai-"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	return strings.ToUpper(s)
}

func countGitHubRepos(cfg *config.Config) int {
	seen := make(map[string]bool)
	if cfg.Adapters != nil && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Repo != "" {
		seen[cfg.Adapters.GitHub.Repo] = true
	}
	for _, proj := range cfg.Projects {
		if proj.GitHub != nil && proj.GitHub.Owner != "" && proj.GitHub.Repo != "" {
			seen[fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)] = true
		}
	}
	return len(seen)
}
