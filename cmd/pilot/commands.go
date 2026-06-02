// Secondary CLI command constructors extracted from main.go (GH-1215)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/config"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Pilot daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Daemon process management is out of scope - users should use
			// standard OS signals (Ctrl+C) or process managers (systemd, launchd)
			fmt.Println("🛑 Stopping Pilot daemon...")
			fmt.Println("   Use Ctrl+C or send SIGTERM to stop the daemon")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Pilot status and running tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config to get gateway address
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if jsonOutput {
				status := map[string]interface{}{
					"gateway": fmt.Sprintf("http://%s:%d", cfg.Gateway.Host, cfg.Gateway.Port),
					"adapters": map[string]bool{
						"linear":   cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled,
						"slack":    cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled,
						"telegram": cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled,
						"github":   cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled,
						"jira":     cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled,
					},
					"projects": cfg.Projects,
				}

				data, err := json.MarshalIndent(status, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal status: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			fmt.Println("📊 Pilot Status")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Gateway: http://%s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
			fmt.Println()

			// Check adapters
			fmt.Println("Adapters:")
			if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
				fmt.Println("  ✓ Linear (enabled)")
			} else {
				fmt.Println("  ○ Linear (disabled)")
			}
			if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
				fmt.Println("  ✓ Slack (enabled)")
			} else {
				fmt.Println("  ○ Slack (disabled)")
			}
			if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
				fmt.Println("  ✓ Telegram (enabled)")
			} else {
				fmt.Println("  ○ Telegram (disabled)")
			}
			if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
				fmt.Println("  ✓ GitHub (enabled)")
			} else {
				fmt.Println("  ○ GitHub (disabled)")
			}
			fmt.Println()

			// List projects
			fmt.Println("Projects:")
			if len(cfg.Projects) == 0 {
				fmt.Println("  (none configured)")
			} else {
				for _, proj := range cfg.Projects {
					nav := ""
					if proj.Navigator {
						nav = " [Navigator]"
					}
					fmt.Printf("  • %s: %s%s\n", proj.Name, proj.Path, nav)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func newInitCmd() *cobra.Command {
	var force bool
	var projectMode bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Pilot configuration or scaffold a project",
		Long: `Initialize Pilot configuration or scaffold a project with CLAUDE.md.

Without flags: initialize ~/.pilot/config.yaml (global Pilot config).
With --project: run the interactive project scaffolding wizard in the current directory.

The --project wizard:
  - Detects the project language (Go, TypeScript, Python)
  - Generates CLAUDE.md with coding conventions and quality gates
  - Adds the project to ~/.pilot/config.yaml
  - Optionally creates a .agent/ Navigator structure

Examples:
  pilot init            # Initialize global config
  pilot init --project  # Scaffold project in current directory
  pilot init --force    # Reinitialize global config (backs up existing)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Project scaffolding mode
			if projectMode {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current directory: %w", err)
				}
				return runInitProject(cwd)
			}

			configPath := config.DefaultConfigPath()

			// Check if config already exists
			if _, err := os.Stat(configPath); err == nil {
				if force {
					// Backup existing config
					backupPath := configPath + ".bak"
					if err := os.Rename(configPath, backupPath); err != nil {
						return fmt.Errorf("failed to backup config: %w", err)
					}
					fmt.Printf("   📦 Backed up existing config to %s\n\n", backupPath)
				} else {
					// Load and display existing config summary
					return showExistingConfigInfo(configPath)
				}
			}

			// Create default config
			cfg := config.DefaultConfig()

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			// Show banner
			banner.PrintWithVersion(version)

			fmt.Println("   ✅ Initialized!")
			fmt.Printf("   Config: %s\n", configPath)
			fmt.Println()
			fmt.Println("   Next steps:")
			fmt.Println("   1. Edit config with your API keys")
			fmt.Println("   2. Add your projects")
			fmt.Println("   3. Run 'pilot start'")

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Reinitialize config (backs up existing to .bak)")
	cmd.Flags().BoolVar(&projectMode, "project", false, "Scaffold a project in the current directory (generates CLAUDE.md)")

	return cmd
}

// showExistingConfigInfo displays a summary of the existing config and helpful options
func showExistingConfigInfo(configPath string) error {
	// Load existing config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Use ~ for home directory in display
	displayPath := configPath
	if home, err := os.UserHomeDir(); err == nil {
		displayPath = strings.Replace(configPath, home, "~", 1)
	}

	fmt.Printf("⚠️  Config already exists: %s\n\n", displayPath)
	fmt.Println("   Current settings:")

	// Projects count
	switch projectCount := len(cfg.Projects); projectCount {
	case 0:
		fmt.Println("   • Projects: none configured")
	case 1:
		fmt.Println("   • Projects: 1 configured")
	default:
		fmt.Printf("   • Projects: %d configured\n", projectCount)
	}

	// Check enabled adapters
	if cfg.Adapters != nil {
		if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
			fmt.Println("   • Telegram: enabled")
		} else {
			fmt.Println("   • Telegram: disabled")
		}

		if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
			fmt.Println("   • GitHub: enabled")
		} else {
			fmt.Println("   • GitHub: disabled")
		}

		if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
			fmt.Println("   • Linear: enabled")
		}

		if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
			fmt.Println("   • Slack: enabled")
		}

		if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
			fmt.Println("   • GitLab: enabled")
		}

		if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
			fmt.Println("   • Jira: enabled")
		}

		if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
			fmt.Println("   • Asana: enabled")
		}

		if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
			fmt.Println("   • Azure DevOps: enabled")
		}
	}

	fmt.Println()
	fmt.Println("   Options:")
	fmt.Printf("   • Edit:   $EDITOR %s\n", displayPath)
	fmt.Println("   • Reset:  pilot init --force")
	fmt.Println("   • Start:  pilot start --help")

	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show Pilot version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Pilot %s\n", version)
			if buildTime != "unknown" {
				fmt.Printf("Built: %s\n", buildTime)
			}
		},
	}
}
