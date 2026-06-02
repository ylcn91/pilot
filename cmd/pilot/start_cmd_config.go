package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
)

// startContext carries the resolved configuration and mode flags produced by
// prepareStart and consumed by the gateway-mode portion of newStartCmd.
type startContext struct {
	cfg              *config.Config
	projectPath      string
	hasTelegram      bool
	hasGithubPolling bool
	hasSlack         bool
}

// prepareStart performs the config-load and mode-selection phase of the
// `pilot start` RunE closure: it loads config, validates and applies flag
// overrides, initializes logging, resolves the project path, determines which
// adapters are enabled, and applies sequential/env overrides. When a polling
// adapter is enabled (or --no-gateway is set) it runs polling mode directly.
//
// The boolean return is a "handled" signal that reproduces the original
// control flow exactly: when true, the caller MUST return the accompanying
// error verbatim (this covers every early `return` in the original prep phase,
// including the `return runPollingMode(...)` whose result — possibly nil — is
// passed straight back). When false, err is nil and the returned context drives
// the remaining gateway-mode setup.
func prepareStart(cmd *cobra.Command, f *startFlags) (*startContext, bool, error) {
	// Load config
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, true, fmt.Errorf("failed to load config: %w", err)
	}

	// GH-2361: Fail loudly when an adapter flag is used but the
	// corresponding adapter block is missing from config. Previously,
	// `pilot start --github` would silently auto-enable a defaulted
	// adapter with no token/repo and poll nothing.
	if err := validateAdapterFlags(cfg, cmd); err != nil {
		return nil, true, err
	}

	// Apply flag overrides to config
	applyInputOverrides(cfg, cmd, f.enableTelegram, f.enableGithub, f.enableLinear, f.enableSlack, f.enableTunnel, f.enablePlane, f.enableDiscord)

	// Apply team ID override if flag provided
	if f.teamID != "" {
		cfg.TeamID = f.teamID
	}

	// Apply team flag overrides (GH-635)
	applyTeamOverrides(cfg, cmd, f.teamID, f.teamMember)

	// Initialize logging with config (GH-847)
	// Apply log-format flag override if set
	if cmd.Flags().Changed("log-format") {
		if cfg.Logging == nil {
			cfg.Logging = logging.DefaultConfig()
		}
		cfg.Logging.Format = f.logFormat
	}
	if cfg.Logging != nil {
		if err := logging.Init(cfg.Logging); err != nil {
			return nil, true, fmt.Errorf("failed to initialize logging: %w", err)
		}
	}

	// GH-879: Log config reload on hot upgrade
	// After syscall.Exec, the new binary starts fresh and re-reads config from disk
	if os.Getenv("PILOT_RESTARTED") == "1" {
		logging.WithComponent("config").Info("config reloaded from disk after hot upgrade",
			"path", configPath)
	}

	// GH-710: Validate Slack Socket Mode config — degrade gracefully if app_token missing
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.SocketMode && cfg.Adapters.Slack.AppToken == "" {
		logging.WithComponent("slack").Warn("socket_mode enabled but app_token not configured, skipping Slack Socket Mode")
		cfg.Adapters.Slack.SocketMode = false
	}

	// Stamp build version into executor config for feature matrix updates (GH-1388)
	if cfg.Executor == nil {
		cfg.Executor = executor.DefaultBackendConfig()
	}
	cfg.Executor.Version = version

	// Resolve project path: flag > config default > cwd
	projectPath := f.projectPath
	if projectPath == "" {
		if defaultProj := cfg.GetDefaultProject(); defaultProj != nil {
			projectPath = defaultProj.Path
		}
	}
	if projectPath == "" {
		cwd, _ := os.Getwd()
		projectPath = cwd
	}
	if strings.HasPrefix(projectPath, "~") {
		home, _ := os.UserHomeDir()
		projectPath = strings.Replace(projectPath, "~", home, 1)
	}

	// Clean stale pilot hooks on startup (GH-1883)
	cleanStartupHooks(cfg, projectPath)

	// Determine mode based on what's enabled
	hasTelegram := cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled
	hasGithubPolling := cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
		cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled
	hasSlack := cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.SocketMode

	// Apply execution mode override from CLI flags
	if f.sequential {
		if cfg.Orchestrator.Execution == nil {
			cfg.Orchestrator.Execution = config.DefaultExecutionConfig()
		}
		cfg.Orchestrator.Execution.Mode = "sequential"
	}

	// Override autopilot config if flag provided
	if f.envFlag != "" {
		if cfg.Orchestrator.Autopilot == nil {
			cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
		}
		cfg.Orchestrator.Autopilot.Enabled = true

		// Use SetActiveEnvironment to validate and resolve environment
		if err := cfg.Orchestrator.Autopilot.SetActiveEnvironment(f.envFlag); err != nil {
			// Show helpful error with available environments
			availableEnvs := []string{"dev", "stage", "prod"}
			if cfg.Orchestrator.Autopilot.Environments != nil {
				for name := range cfg.Orchestrator.Autopilot.Environments {
					availableEnvs = append(availableEnvs, name)
				}
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "Available environments: %v\n", availableEnvs)
			fmt.Fprintf(os.Stderr, "\nTo add a custom environment, add to autopilot.environments in config.yaml:\n")
			fmt.Fprintf(os.Stderr, "autopilot:\n  environments:\n    my-env:\n      branch: main\n      require_approval: true\n")
			return nil, true, err
		}
	}

	// GH-394: Polling mode is the default when any polling adapter is enabled.
	// Previously, having linear.enabled=true would force gateway mode even when
	// only using GitHub/Telegram polling. Now polling adapters work independently.
	//
	// Mode selection:
	// - noGateway flag: always use polling mode (user override)
	// - Polling adapters enabled: use polling mode (Telegram, GitHub)
	// - Only webhook adapters (Linear, Jira): use gateway mode
	//
	// Note: Linear/Jira webhooks require gateway but don't block polling adapters.
	// When both are needed, gateway starts in background within polling mode.
	// Splash screen removed — caused alt-screen flicker between
	// splash exit and dashboard start (GH-2459 follow-up).

	hasPollingAdapter := hasTelegram || hasGithubPolling
	if f.noGateway || hasPollingAdapter {
		return nil, true, runPollingMode(cmd, cfg, projectPath, f.replace, f.dashboardMode, f.noGateway)
	}

	return &startContext{
		cfg:              cfg,
		projectPath:      projectPath,
		hasTelegram:      hasTelegram,
		hasGithubPolling: hasGithubPolling,
		hasSlack:         hasSlack,
	}, false, nil
}
