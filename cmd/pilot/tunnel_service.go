package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/tunnel"
)

func newTunnelSetupCmd() *cobra.Command {
	var provider string
	var domain string
	var installService bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up tunnel (create tunnel, configure DNS)",
		Long: `Set up Cloudflare Tunnel for permanent webhook URLs.

This command:
1. Checks for cloudflared CLI installation
2. Authenticates with Cloudflare (if needed)
3. Creates a tunnel named 'pilot-webhook'
4. Configures DNS routing (if custom domain provided)
5. Optionally installs auto-start service

Prerequisites:
  - Cloudflare account (free tier is sufficient)
  - cloudflared CLI: brew install cloudflared

Examples:
  pilot tunnel setup                           # Basic setup
  pilot tunnel setup --domain pilot.example.com  # With custom domain
  pilot tunnel setup --service                 # With auto-start service`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			// Update config with flags
			tunnelCfg := getTunnelConfig(cfg)
			if provider != "" {
				tunnelCfg.Provider = provider
			}
			if domain != "" {
				tunnelCfg.Domain = domain
			}
			if cfg.Gateway != nil {
				tunnelCfg.Port = cfg.Gateway.Port
			}

			// Check CLI installation
			fmt.Printf("Checking %s installation... ", tunnelCfg.Provider)
			manager, err := tunnel.NewManager(tunnelCfg, slog.Default())
			if err != nil {
				fmt.Println("✗")
				return fmt.Errorf("failed to create tunnel manager: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			status, _ := manager.Status(ctx)
			if status != nil && !status.Running {
				fmt.Println("✓")
			} else {
				fmt.Println("✓ (already running)")
			}

			// Run setup
			fmt.Println("Setting up tunnel...")
			if err := manager.Setup(ctx); err != nil {
				return fmt.Errorf("setup failed: %w", err)
			}

			// Save tunnel config
			tunnelCfg.Enabled = true
			if cfg.Adapters == nil {
				cfg.Adapters = &config.AdaptersConfig{}
			}

			// Save config
			configPath := config.DefaultConfigPath()
			if err := config.Save(cfg, configPath); err != nil {
				fmt.Printf("Warning: failed to save config: %v\n", err)
			}

			fmt.Println("✓ Tunnel configured")

			// Install service if requested
			if installService {
				fmt.Print("Installing auto-start service... ")

				// Get tunnel ID from status
				ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel2()
				status, _ := manager.Status(ctx2)
				if status != nil && status.TunnelID != "" {
					if err := tunnel.InstallService(status.TunnelID); err != nil {
						fmt.Println("✗")
						fmt.Printf("Warning: failed to install service: %v\n", err)
					} else {
						fmt.Println("✓")
						fmt.Println("  Tunnel will auto-start on boot")
					}
				} else {
					fmt.Println("✗")
					fmt.Println("Warning: could not get tunnel ID for service installation")
				}
			}

			// Show result
			fmt.Println()
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("Setup complete!")
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  pilot tunnel start          # Start the tunnel")
			fmt.Println("  pilot tunnel status         # Check status")
			if !installService {
				fmt.Println("  pilot tunnel service install # Auto-start on boot")
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "cloudflare", "Tunnel provider (cloudflare, ngrok)")
	cmd.Flags().StringVar(&domain, "domain", "", "Custom domain (optional)")
	cmd.Flags().BoolVar(&installService, "service", false, "Install auto-start service")

	return cmd
}

func newTunnelServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage tunnel auto-start service",
	}

	cmd.AddCommand(
		newTunnelServiceInstallCmd(),
		newTunnelServiceUninstallCmd(),
		newTunnelServiceStatusCmd(),
	)

	return cmd
}

func newTunnelServiceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install auto-start service (macOS only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			tunnelCfg := getTunnelConfig(cfg)
			manager, err := tunnel.NewManager(tunnelCfg, slog.Default())
			if err != nil {
				return fmt.Errorf("failed to create tunnel manager: %w", err)
			}

			// Get tunnel ID
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			status, err := manager.Status(ctx)
			if err != nil {
				return fmt.Errorf("failed to get tunnel status: %w", err)
			}

			if status.TunnelID == "" {
				return fmt.Errorf("no tunnel configured - run 'pilot tunnel setup' first")
			}

			fmt.Print("Installing service... ")
			if err := tunnel.InstallService(status.TunnelID); err != nil {
				fmt.Println("✗")
				return fmt.Errorf("failed to install service: %w", err)
			}
			fmt.Println("✓")

			fmt.Println()
			fmt.Println("Service installed!")
			fmt.Println("  - Tunnel will auto-start on boot")
			fmt.Println("  - Run 'pilot tunnel service status' to check")
			fmt.Println()

			return nil
		},
	}
}

func newTunnelServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove auto-start service",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print("Uninstalling service... ")
			if err := tunnel.UninstallService(); err != nil {
				fmt.Println("✗")
				return fmt.Errorf("failed to uninstall service: %w", err)
			}
			fmt.Println("✓")

			fmt.Println("Service removed - tunnel will no longer auto-start")
			return nil
		},
	}
}

func newTunnelServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			status := tunnel.GetServiceStatus()

			fmt.Println()
			fmt.Println("Service Status (launchd)")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			if status.Installed {
				fmt.Println("  Installed: ✓ Yes")
				fmt.Printf("  Path:      %s\n", status.PlistPath)
				if status.Running {
					fmt.Println("  Running:   ✓ Yes")
				} else {
					fmt.Println("  Running:   ○ No")
				}
			} else {
				fmt.Println("  Installed: ○ No")
				fmt.Println()
				fmt.Println("  Run 'pilot tunnel service install' to enable auto-start")
			}

			fmt.Println()
			return nil
		},
	}
}

// getTunnelConfig extracts tunnel config from main config
func getTunnelConfig(cfg *config.Config) *tunnel.Config {
	// Use configured tunnel settings if available
	if cfg.Tunnel != nil {
		tunnelCfg := cfg.Tunnel
		// Default port from gateway if not set
		if tunnelCfg.Port == 0 && cfg.Gateway != nil {
			tunnelCfg.Port = cfg.Gateway.Port
		}
		return tunnelCfg
	}

	// Fall back to defaults
	tunnelCfg := tunnel.DefaultConfig()
	if cfg.Gateway != nil {
		tunnelCfg.Port = cfg.Gateway.Port
	}
	return tunnelCfg
}
