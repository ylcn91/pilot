package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/config"
)

func newAutopilotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autopilot",
		Short: "Autopilot commands for PR lifecycle management",
		Long:  `Commands for viewing and managing autopilot PR tracking and automation.`,
	}

	cmd.AddCommand(
		newAutopilotStatusCmd(),
		newAutopilotListCmd(),
		newAutopilotEnableCmd(),
		newAutopilotDisableCmd(),
	)
	return cmd
}

func newAutopilotStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show tracked PRs and their current stage",
		Long: `Display autopilot status including:
- Tracked PRs and their lifecycle stage
- Time in current stage
- CI status for each PR
- Release configuration status

This command queries the running Pilot instance for autopilot state.
Note: Pilot must be running with --env flag for this to work.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Check if autopilot is configured
			if cfg.Orchestrator == nil || cfg.Orchestrator.Autopilot == nil || !cfg.Orchestrator.Autopilot.Enabled {
				if jsonOutput {
					data := map[string]interface{}{
						"enabled": false,
						"error":   "autopilot not enabled in config",
					}
					out, _ := json.MarshalIndent(data, "", "  ")
					fmt.Println(string(out))
					return nil
				}
				fmt.Println("⚠️  Autopilot is not enabled in configuration")
				fmt.Println("   Start Pilot with --env=<env> to enable autopilot mode")
				return nil
			}

			autopilotCfg := cfg.Orchestrator.Autopilot

			if jsonOutput {
				data := map[string]interface{}{
					"enabled":     true,
					"environment": autopilotCfg.Environment,
					"auto_merge":  autopilotCfg.AutoMerge,
					"auto_review": autopilotCfg.AutoReview,
					"release": map[string]interface{}{
						"enabled": autopilotCfg.Release != nil && autopilotCfg.Release.Enabled,
						"trigger": func() string {
							if autopilotCfg.Release != nil {
								return autopilotCfg.Release.Trigger
							}
							return ""
						}(),
						"requireCI": func() bool {
							if autopilotCfg.Release != nil {
								return autopilotCfg.Release.RequireCI
							}
							return false
						}(),
					},
					"ci_wait_timeout": autopilotCfg.CIWaitTimeout.String(),
					"max_failures":    autopilotCfg.MaxFailures,
					"note":            "For live PR tracking, check the dashboard or logs. This shows config only.",
				}
				out, _ := json.MarshalIndent(data, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			fmt.Println("🤖 Autopilot Status")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Environment: %s\n", autopilotCfg.EnvironmentName())
			fmt.Println()

			fmt.Println("Configuration:")
			fmt.Printf("  Auto Merge:     %v\n", autopilotCfg.AutoMerge)
			fmt.Printf("  Auto Review:    %v\n", autopilotCfg.AutoReview)
			fmt.Printf("  Merge Method:   %s\n", autopilotCfg.MergeMethod)
			fmt.Printf("  CI Timeout:     %s\n", autopilotCfg.CIWaitTimeout)
			fmt.Printf("  Max Failures:   %d\n", autopilotCfg.MaxFailures)
			fmt.Println()

			fmt.Println("Release:")
			if autopilotCfg.Release != nil && autopilotCfg.Release.Enabled {
				fmt.Printf("  Enabled:        true\n")
				fmt.Printf("  Trigger:        %s\n", autopilotCfg.Release.Trigger)
				fmt.Printf("  Require CI:     %v\n", autopilotCfg.Release.RequireCI)
				fmt.Printf("  Tag Prefix:     %s\n", autopilotCfg.Release.TagPrefix)
			} else {
				fmt.Printf("  Enabled:        false\n")
			}
			fmt.Println()

			fmt.Println("ℹ️  For live PR tracking, check:")
			fmt.Println("   • Dashboard: pilot start --dashboard --env=<env>")
			fmt.Println("   • Logs: pilot logs --follow")

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func newAutopilotListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured autopilot environments",
		Long: `Display all configured autopilot environments and their settings.

Shows both built-in environments (dev, stage, prod) and any custom environments
defined in the config file under autopilot.environments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Check if autopilot is configured
			if cfg.Orchestrator == nil || cfg.Orchestrator.Autopilot == nil {
				if jsonOutput {
					data := map[string]interface{}{
						"environments": []string{},
						"message":      "autopilot not configured",
					}
					out, _ := json.MarshalIndent(data, "", "  ")
					fmt.Println(string(out))
					return nil
				}
				fmt.Println("⚠️  Autopilot is not configured")
				fmt.Println("   Enable autopilot: pilot autopilot enable")
				return nil
			}

			autopilotCfg := cfg.Orchestrator.Autopilot

			// Collect all environments: built-in defaults + custom
			envMap := make(map[string]*autopilot.EnvironmentConfig)

			// Add custom environments from config
			if autopilotCfg.Environments != nil {
				for name, envCfg := range autopilotCfg.Environments {
					envMap[name] = envCfg
				}
			}

			if jsonOutput {
				envList := make([]map[string]interface{}, 0)
				for name, envCfg := range envMap {
					envList = append(envList, map[string]interface{}{
						"name":               name,
						"branch":             envCfg.Branch,
						"require_approval":   envCfg.RequireApproval,
						"approval_source":    string(envCfg.ApprovalSource),
						"ci_timeout":         envCfg.CITimeout.String(),
						"skip_post_merge_ci": envCfg.SkipPostMergeCI,
						"merge_method":       envCfg.MergeMethod,
					})
				}
				data := map[string]interface{}{
					"environments": envList,
				}
				out, _ := json.MarshalIndent(data, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			fmt.Println("🤖 Configured Autopilot Environments")
			fmt.Println("───────────────────────────────────────")
			if len(envMap) == 0 {
				fmt.Println("No environments configured (using defaults)")
				fmt.Println()
				fmt.Println("Built-in defaults:")
				fmt.Println("  dev    → main, no approval, 5m CI timeout")
				fmt.Println("  stage  → main, no approval, 30m CI timeout")
				fmt.Println("  prod   → main, approval required, 30m CI timeout")
				return nil
			}

			for name, envCfg := range envMap {
				fmt.Printf("  %s\n", name)
				fmt.Printf("    Branch:    %s\n", envCfg.Branch)
				fmt.Printf("    Approval:  %v\n", envCfg.RequireApproval)
				if envCfg.RequireApproval && envCfg.ApprovalSource != "" {
					fmt.Printf("    via:       %s\n", envCfg.ApprovalSource)
				}
				fmt.Printf("    CI Timeout: %s\n", envCfg.CITimeout)
				if envCfg.MergeMethod != "" {
					fmt.Printf("    Merge:     %s\n", envCfg.MergeMethod)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func newAutopilotEnableCmd() *cobra.Command {
	var (
		env        string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable autopilot in configuration",
		Long: `Enable autopilot mode in Pilot configuration.

This updates the config file to enable autopilot. You must restart Pilot
for changes to take effect.

Examples:
  pilot autopilot enable                 # Enable with default (dev) environment
  pilot autopilot enable --env=stage     # Enable with staging environment
  pilot autopilot enable --env=prod      # Enable with production environment`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Validate environment
			switch autopilot.Environment(env) {
			case autopilot.EnvDev, autopilot.EnvStage, autopilot.EnvProd:
				// valid
			default:
				return fmt.Errorf("invalid environment: %s (use: dev, stage, prod)", env)
			}

			// Initialize orchestrator config if nil
			if cfg.Orchestrator == nil {
				cfg.Orchestrator = &config.OrchestratorConfig{}
			}

			// Initialize autopilot config if nil
			if cfg.Orchestrator.Autopilot == nil {
				cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
			}

			// Enable autopilot
			cfg.Orchestrator.Autopilot.Enabled = true
			cfg.Orchestrator.Autopilot.Environment = autopilot.Environment(env)

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			if jsonOutput {
				data := map[string]interface{}{
					"enabled":     true,
					"environment": env,
					"message":     "autopilot enabled",
				}
				out, _ := json.MarshalIndent(data, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			fmt.Printf("✓ Autopilot enabled (environment: %s)\n", env)
			fmt.Println("  Restart Pilot to apply: pilot start --env=" + env)
			return nil
		},
	}

	cmd.Flags().StringVar(&env, "env", "dev", "Environment: dev, stage, prod")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func newAutopilotDisableCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable autopilot in configuration",
		Long: `Disable autopilot mode in Pilot configuration.

This updates the config file to disable autopilot. You must restart Pilot
for changes to take effect.

Examples:
  pilot autopilot disable            # Disable autopilot
  pilot autopilot disable --json     # Output as JSON`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Check if already disabled
			if cfg.Orchestrator == nil || cfg.Orchestrator.Autopilot == nil || !cfg.Orchestrator.Autopilot.Enabled {
				if jsonOutput {
					data := map[string]interface{}{
						"enabled": false,
						"message": "autopilot already disabled",
					}
					out, _ := json.MarshalIndent(data, "", "  ")
					fmt.Println(string(out))
					return nil
				}
				fmt.Println("Autopilot is already disabled")
				return nil
			}

			// Disable autopilot
			cfg.Orchestrator.Autopilot.Enabled = false

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			if jsonOutput {
				data := map[string]interface{}{
					"enabled": false,
					"message": "autopilot disabled",
				}
				out, _ := json.MarshalIndent(data, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			fmt.Println("✓ Autopilot disabled")
			fmt.Println("  Restart Pilot to apply changes")
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}
