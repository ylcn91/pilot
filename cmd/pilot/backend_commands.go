package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

func newBackendStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current backend configuration",
		Long: `Show current backend configuration and health.

Example output:
  Active backend: codex-exec
  Command: codex
  Version: codex-cli 0.0.0
  Status: ✓ ready`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				cfg = config.DefaultConfig()
			}

			activeBackend := executor.BackendTypeCodexExec
			if cfg.Executor != nil && cfg.Executor.Type != "" {
				activeBackend = cfg.Executor.Type
			}

			// Find backend info
			var info *backendInfo
			for i := range supportedBackends {
				if supportedBackends[i].Name == activeBackend {
					info = &supportedBackends[i]
					break
				}
			}

			if info == nil {
				return fmt.Errorf("unknown backend type: %s", activeBackend)
			}

			// Get the actual command from config
			command := info.Command
			if cfg.Executor != nil {
				switch activeBackend {
				case executor.BackendTypeClaudeCode:
					if cfg.Executor.ClaudeCode != nil && cfg.Executor.ClaudeCode.Command != "" {
						command = cfg.Executor.ClaudeCode.Command
					}
				case executor.BackendTypeCodexExec:
					if cfg.Executor.CodexExec != nil && cfg.Executor.CodexExec.Command != "" {
						command = cfg.Executor.CodexExec.Command
					}
				case executor.BackendTypeQwenCode:
					if cfg.Executor.QwenCode != nil && cfg.Executor.QwenCode.Command != "" {
						command = cfg.Executor.QwenCode.Command
					}
				case executor.BackendTypeOpenCode:
					if cfg.Executor.OpenCode != nil && cfg.Executor.OpenCode.ServerCommand != "" {
						parts := strings.Fields(cfg.Executor.OpenCode.ServerCommand)
						if len(parts) > 0 {
							command = parts[0]
						}
					}
				}
			}

			// Check installation and version
			_, lookErr := exec.LookPath(command)
			installed := lookErr == nil

			version := ""
			if installed {
				version = info.getVersion(command)
			}

			status := "✗ not ready (CLI not found)"
			if installed {
				status = "✓ ready"
			}

			fmt.Printf("Active backend: %s\n", activeBackend)
			fmt.Printf("Command: %s\n", command)
			if version != "" {
				fmt.Printf("Version: %s\n", version)
			}
			fmt.Printf("Status: %s\n", status)
			fmt.Println()
			fmt.Printf("Config: %s\n", configPath)
			fmt.Printf("  executor.type: %s\n", activeBackend)

			// Show backend-specific config
			if cfg.Executor != nil {
				switch activeBackend {
				case executor.BackendTypeClaudeCode:
					if cfg.Executor.ClaudeCode != nil {
						fmt.Printf("  executor.claude_code.command: %s\n", command)
					}
				case executor.BackendTypeCodexExec:
					if cfg.Executor.CodexExec != nil {
						fmt.Printf("  executor.codex_exec.command: %s\n", command)
						if cfg.Executor.CodexExec.Sandbox != "" {
							fmt.Printf("  executor.codex_exec.sandbox: %s\n", cfg.Executor.CodexExec.Sandbox)
						}
					}
				case executor.BackendTypeQwenCode:
					if cfg.Executor.QwenCode != nil {
						fmt.Printf("  executor.qwen_code.command: %s\n", command)
					}
				case executor.BackendTypeOpenCode:
					if cfg.Executor.OpenCode != nil {
						if cfg.Executor.OpenCode.ServerURL != "" {
							fmt.Printf("  executor.opencode.server_url: %s\n", cfg.Executor.OpenCode.ServerURL)
						}
					}
				}
			}

			return nil
		},
	}
}

func newBackendSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <type>",
		Short: "Set active backend",
		Long: `Switch the active backend in the config file.

Valid types: codex-exec, claude-code, qwen-code, opencode

Example:
  pilot backend set codex-exec
  → Updated executor.type to "codex-exec" in ~/.pilot/config.yaml
  → Verified: codex CLI found at /usr/local/bin/codex`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backendType := args[0]

			// Validate backend type
			validTypes := []string{
				executor.BackendTypeCodexExec,
				executor.BackendTypeClaudeCode,
				executor.BackendTypeQwenCode,
				executor.BackendTypeOpenCode,
			}
			isValid := false
			for _, t := range validTypes {
				if backendType == t {
					isValid = true
					break
				}
			}
			if !isValid {
				return fmt.Errorf("invalid backend type: %s\nValid types: %s", backendType, strings.Join(validTypes, ", "))
			}

			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			// Load existing config
			cfg, err := config.Load(configPath)
			if err != nil {
				// If config doesn't exist, create a default one
				cfg = config.DefaultConfig()
			}

			// Update the backend type
			if cfg.Executor == nil {
				cfg.Executor = executor.DefaultBackendConfig()
			}
			cfg.Executor.Type = backendType

			// Write config back
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			if err := os.WriteFile(configPath, data, 0600); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			fmt.Printf("Updated executor.type to %q in %s\n", backendType, configPath)

			// Verify CLI is installed
			var info *backendInfo
			for i := range supportedBackends {
				if supportedBackends[i].Name == backendType {
					info = &supportedBackends[i]
					break
				}
			}

			if info != nil {
				command := info.Command
				// Get custom command from config if set
				switch backendType {
				case executor.BackendTypeClaudeCode:
					if cfg.Executor.ClaudeCode != nil && cfg.Executor.ClaudeCode.Command != "" {
						command = cfg.Executor.ClaudeCode.Command
					}
				case executor.BackendTypeCodexExec:
					if cfg.Executor.CodexExec != nil && cfg.Executor.CodexExec.Command != "" {
						command = cfg.Executor.CodexExec.Command
					}
				case executor.BackendTypeQwenCode:
					if cfg.Executor.QwenCode != nil && cfg.Executor.QwenCode.Command != "" {
						command = cfg.Executor.QwenCode.Command
					}
				case executor.BackendTypeOpenCode:
					if cfg.Executor.OpenCode != nil && cfg.Executor.OpenCode.ServerCommand != "" {
						parts := strings.Fields(cfg.Executor.OpenCode.ServerCommand)
						if len(parts) > 0 {
							command = parts[0]
						}
					}
				}

				path, err := exec.LookPath(command)
				if err != nil {
					fmt.Printf("Warning: %s CLI not found in PATH\n", command)
				} else {
					fmt.Printf("Verified: %s CLI found at %s\n", command, path)
				}
			}

			return nil
		},
	}
}
