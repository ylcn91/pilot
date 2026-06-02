package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ylcn91/pilot/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Pilot configuration",
		Long:  `View, edit, and validate Pilot configuration.`,
	}

	cmd.AddCommand(
		newConfigShowCmd(),
		newConfigEditCmd(),
		newConfigValidateCmd(),
		newConfigPathCmd(),
	)

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if outputJSON {
				data, err := json.MarshalIndent(cfg, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal config: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			// YAML output
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}
			fmt.Print(string(data))

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open config in editor",
		Long: `Open the Pilot configuration file in your default editor.

Uses $EDITOR environment variable, falling back to:
  - vim (if available)
  - nano (if available)
  - vi (if available)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			// Check if config exists
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				fmt.Printf("Config file does not exist at %s\n", configPath)
				fmt.Println("Run 'pilot init' to create one.")
				return nil
			}

			// Find editor
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = os.Getenv("VISUAL")
			}
			if editor == "" {
				// Try common editors
				for _, e := range []string{"vim", "nano", "vi"} {
					if _, err := exec.LookPath(e); err == nil {
						editor = e
						break
					}
				}
			}
			if editor == "" {
				return fmt.Errorf("no editor found. Set $EDITOR environment variable")
			}

			// Open editor
			editorCmd := exec.Command(editor, configPath)
			editorCmd.Stdin = os.Stdin
			editorCmd.Stdout = os.Stdout
			editorCmd.Stderr = os.Stderr

			if err := editorCmd.Run(); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}

			// Validate after editing
			fmt.Println()
			fmt.Println("Validating configuration...")

			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Printf("Warning: Failed to load config: %v\n", err)
				return nil
			}

			if err := cfg.Validate(); err != nil {
				fmt.Printf("Warning: Config validation failed: %v\n", err)
				return nil
			}

			fmt.Println("Configuration is valid!")

			return nil
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration syntax",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			// Check if file exists
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				if quiet {
					os.Exit(1)
				}
				return fmt.Errorf("config file does not exist: %s", configPath)
			}

			// Try to load
			cfg, err := config.Load(configPath)
			if err != nil {
				if quiet {
					os.Exit(1)
				}
				return fmt.Errorf("invalid YAML syntax: %w", err)
			}

			// Validate
			if err := cfg.Validate(); err != nil {
				if quiet {
					os.Exit(1)
				}
				return fmt.Errorf("validation failed: %w", err)
			}

			// Check for common issues
			var warnings []string

			// Check adapters
			if cfg.Adapters == nil {
				warnings = append(warnings, "No adapters configured")
			} else {
				hasAdapter := false
				if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
					hasAdapter = true
					if cfg.Adapters.Telegram.BotToken == "" {
						warnings = append(warnings, "Telegram enabled but bot_token not set")
					}
				}
				if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
					hasAdapter = true
				}
				if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
					hasAdapter = true
					if cfg.Adapters.Slack.BotToken == "" {
						warnings = append(warnings, "Slack enabled but bot_token not set")
					}
				}
				if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled {
					hasAdapter = true
					if cfg.Adapters.GitHub.Token == "" && os.Getenv("GITHUB_TOKEN") == "" {
						warnings = append(warnings, "GitHub enabled but token not set")
					}
				}
				if !hasAdapter {
					warnings = append(warnings, "No adapters enabled")
				}
			}

			// Check projects
			if len(cfg.Projects) == 0 {
				warnings = append(warnings, "No projects configured")
			} else {
				for _, proj := range cfg.Projects {
					if _, err := os.Stat(proj.Path); os.IsNotExist(err) {
						warnings = append(warnings, fmt.Sprintf("Project path does not exist: %s", proj.Path))
					}
				}
			}

			if quiet {
				return nil
			}

			fmt.Printf("Config: %s\n", configPath)
			fmt.Println()
			fmt.Println("Syntax:     OK")
			fmt.Println("Validation: OK")
			fmt.Println()

			if len(warnings) > 0 {
				fmt.Println("Warnings:")
				for _, w := range warnings {
					fmt.Printf("  - %s\n", w)
				}
			} else {
				fmt.Println("No warnings.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Exit with code 1 on error, no output")

	return cmd
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Show config file path",
		Run: func(cmd *cobra.Command, args []string) {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}
			fmt.Println(configPath)
		},
	}
}
