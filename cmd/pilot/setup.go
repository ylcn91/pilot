package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/config"
)

var noSleep bool

func newSetupCmd() *cobra.Command {
	var skipOptional bool
	var setupTunnel bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard",
		Long: `Interactive wizard to configure Pilot step by step.

Sets up:
  - Telegram bot connection
  - Project paths
  - Voice transcription
  - Daily briefs
  - Alerts
  - Cloudflare Tunnel (with --tunnel flag)

Examples:
  pilot setup              # Full interactive setup
  pilot setup --skip-optional  # Skip optional features
  pilot setup --tunnel     # Set up Cloudflare Tunnel for webhooks`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Note: 'pilot setup' is deprecated. Use 'pilot onboard' instead.")
			fmt.Println()

			// If --tunnel flag, redirect to tunnel setup
			if setupTunnel {
				tunnelCmd := newTunnelSetupCmd()
				return tunnelCmd.RunE(tunnelCmd, args)
			}

			// If --no-sleep flag, disable Mac sleep and exit
			if noSleep {
				return disableMacSleep()
			}

			reader := bufio.NewReader(os.Stdin)

			// Load existing config or create new
			cfg, _ := loadConfig()
			if cfg == nil {
				cfg = config.DefaultConfig()
			}

			fmt.Println()
			fmt.Println("Pilot Setup")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			// Check what's already configured
			hasTelegram := cfg.Adapters != nil && cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.BotToken != ""
			hasProjects := len(cfg.Projects) > 0
			hasVoice := cfg.Adapters != nil && cfg.Adapters.Telegram != nil &&
				cfg.Adapters.Telegram.Transcription != nil &&
				cfg.Adapters.Telegram.Transcription.OpenAIAPIKey != ""
			hasBriefs := cfg.Orchestrator != nil && cfg.Orchestrator.DailyBrief != nil && cfg.Orchestrator.DailyBrief.Enabled
			hasAlerts := cfg.Alerts != nil && cfg.Alerts.Enabled

			// Show current status
			fmt.Println()
			fmt.Println("Current Status:")
			printStatus("Telegram", hasTelegram)
			printStatus("Projects", hasProjects)
			if !skipOptional {
				printStatus("Voice", hasVoice)
				printStatus("Daily Briefs", hasBriefs)
				printStatus("Alerts", hasAlerts)
			}
			fmt.Println()

			// Check if everything is configured
			allConfigured := hasTelegram && hasProjects
			if !skipOptional {
				allConfigured = allConfigured && hasVoice && hasBriefs && hasAlerts
			}

			if allConfigured {
				fmt.Println("✅ Everything is configured!")
				fmt.Println()
				fmt.Print("Reconfigure anyway? [y/N]: ")
				if !readYesNo(reader, false) {
					fmt.Println()
					fmt.Println("Run 'pilot doctor' to verify configuration")
					return nil
				}
				fmt.Println()
			}

			// Only setup unconfigured items (or all if user chose to reconfigure)
			needsSetup := !allConfigured

			// Telegram Bot
			if needsSetup && !hasTelegram {
				fmt.Println("Telegram Bot")
				fmt.Println("─────────────────────────")
				if err := setupTelegram(reader, cfg); err != nil {
					return err
				}
				fmt.Println()
			}

			// Projects
			if needsSetup && !hasProjects {
				fmt.Println("Projects")
				fmt.Println("─────────────────────────")
				if err := setupProjects(reader, cfg); err != nil {
					return err
				}
				fmt.Println()
			}

			if !skipOptional {
				// Voice Transcription
				if needsSetup && !hasVoice {
					fmt.Println("Voice Transcription")
					fmt.Println("─────────────────────────")
					if err := setupVoice(reader, cfg); err != nil {
						return err
					}
					fmt.Println()
				}

				// Daily Briefs
				if needsSetup && !hasBriefs {
					fmt.Println("Daily Briefs")
					fmt.Println("─────────────────────────")
					if err := setupBriefs(reader, cfg); err != nil {
						return err
					}
					fmt.Println()
				}

				// Alerts
				if needsSetup && !hasAlerts {
					fmt.Println("Alerts")
					fmt.Println("─────────────────────────")
					if err := setupAlerts(reader, cfg); err != nil {
						return err
					}
					fmt.Println()
				}
			}

			// Save config
			configPath := config.DefaultConfigPath()
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("Setup complete!")
			fmt.Println()
			fmt.Printf("Config saved to: %s\n", configPath)
			fmt.Println()
			fmt.Println("Next steps:")
			if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
				fmt.Println("  pilot telegram    # Start Telegram bot")
			}
			fmt.Println("  pilot doctor      # Verify configuration")
			fmt.Println("  pilot task \"...\"  # Execute a task")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().BoolVar(&skipOptional, "skip-optional", false, "Skip optional feature setup")
	cmd.Flags().BoolVar(&setupTunnel, "tunnel", false, "Set up Cloudflare Tunnel for webhooks (runs pilot tunnel setup)")

	cmd.Flags().BoolVar(&noSleep, "no-sleep", false, "Disable Mac sleep for always-on operation (macOS only, requires sudo)")

	return cmd
}

// disableMacSleep disables system sleep on macOS for always-on operation
func disableMacSleep() error {
	if runtime.GOOS != "darwin" {
		fmt.Println("⚠️  --no-sleep only works on macOS")
		return nil
	}

	fmt.Println("🔋 Disabling Mac sleep...")
	fmt.Println()
	fmt.Println("This requires administrator privileges.")
	fmt.Println("You may be prompted for your password.")
	fmt.Println()

	cmd := exec.Command("sudo", "pmset", "-a", "sleep", "0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to disable sleep: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ Mac sleep disabled")
	fmt.Println()
	fmt.Println("Your Mac will no longer sleep automatically.")
	fmt.Println("To re-enable sleep later:")
	fmt.Println("  sudo pmset -a sleep 1")
	fmt.Println()

	return nil
}
