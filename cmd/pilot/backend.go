package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

// backendInfo holds information about a supported backend
type backendInfo struct {
	Name       string
	Command    string
	ConfigKey  string
	getVersion func(cmd string) string
}

var supportedBackends = []backendInfo{
	{
		Name:      executor.BackendTypeCodexExec,
		Command:   "codex",
		ConfigKey: "codex_exec",
		getVersion: func(cmd string) string {
			out, err := exec.Command(cmd, "--version").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	},
	{
		Name:      executor.BackendTypeClaudeCode,
		Command:   "claude",
		ConfigKey: "claude_code",
		getVersion: func(cmd string) string {
			out, err := exec.Command(cmd, "--version").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	},
	{
		Name:      executor.BackendTypeQwenCode,
		Command:   "qwen",
		ConfigKey: "qwen_code",
		getVersion: func(cmd string) string {
			out, err := exec.Command(cmd, "--version").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	},
	{
		Name:      executor.BackendTypeOpenCode,
		Command:   "opencode",
		ConfigKey: "opencode",
		getVersion: func(cmd string) string {
			out, err := exec.Command(cmd, "--version").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	},
}

func newBackendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backend",
		Short: "Manage execution backends",
		Long: `Manage AI execution backends (Codex Exec, Claude Code, Qwen Code, OpenCode).

List supported backends, check their status, and switch the active backend.`,
	}

	cmd.AddCommand(
		newBackendListCmd(),
		newBackendStatusCmd(),
		newBackendSetCmd(),
	)

	return cmd
}

func newBackendListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all supported backends",
		Long: `Show all supported backends and whether their CLI is installed.

Example output:
  Backend        Status      Command    Config
  codex-exec     ✓ installed codex      (default)
  claude-code    ✓ installed claude
  qwen-code      ✗ missing   qwen
  opencode       ✓ installed opencode  `,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config to check which is default
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				// Config doesn't exist yet, use defaults
				cfg = config.DefaultConfig()
			}

			activeBackend := executor.BackendTypeCodexExec
			if cfg.Executor != nil && cfg.Executor.Type != "" {
				activeBackend = cfg.Executor.Type
			}

			// Print header
			fmt.Printf("%-14s %-12s %-10s %s\n", "Backend", "Status", "Command", "Config")

			for _, backend := range supportedBackends {
				// Get the actual command from config or use default
				command := backend.Command
				if cfg.Executor != nil {
					switch backend.Name {
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
						// OpenCode uses server command
						if cfg.Executor.OpenCode != nil && cfg.Executor.OpenCode.ServerCommand != "" {
							parts := strings.Fields(cfg.Executor.OpenCode.ServerCommand)
							if len(parts) > 0 {
								command = parts[0]
							}
						}
					}
				}

				// Check if installed
				_, err := exec.LookPath(command)
				installed := err == nil

				status := "✗ missing"
				if installed {
					status = "✓ installed"
				}

				configNote := ""
				if backend.Name == activeBackend {
					configNote = "(default)"
				}

				fmt.Printf("%-14s %-12s %-10s %s\n", backend.Name, status, command, configNote)
			}

			return nil
		},
	}
}
