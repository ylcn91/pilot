package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
		newConfigSetCmd(),
	)

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value> [<key> <value>...]",
		Short: "Set configuration values",
		Long: `Set one or more configuration values and validate the result.

Values are typed automatically: true/false become booleans, numbers become
numbers, and JSON/YAML flow values like ["Bug","Task"] become sequences.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 || len(args)%2 != 0 {
				return fmt.Errorf("expected key/value pairs")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			doc, err := loadConfigDocument(configPath)
			if err != nil {
				return err
			}

			root := configDocumentRoot(doc)
			for i := 0; i < len(args); i += 2 {
				key := strings.TrimSpace(args[i])
				if key == "" {
					return fmt.Errorf("config key cannot be empty")
				}
				if err := setYAMLPath(root, key, configValueNode(args[i+1])); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			}

			data, err := yaml.Marshal(doc)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			cfg := config.DefaultConfig()
			if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), cfg); err != nil {
				return fmt.Errorf("failed to parse updated config: %w", err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("updated config is invalid: %w", err)
			}

			if err := writeConfigBytes(configPath, data); err != nil {
				return err
			}

			fmt.Printf("Updated %s\n", configPath)
			for i := 0; i < len(args); i += 2 {
				fmt.Printf("  %s\n", args[i])
			}
			return nil
		},
	}
}

func loadConfigDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		defaultData, marshalErr := yaml.Marshal(config.DefaultConfig())
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to build default config: %w", marshalErr)
		}
		data = defaultData
	}
	if strings.TrimSpace(string(data)) == "" {
		defaultData, marshalErr := yaml.Marshal(config.DefaultConfig())
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to build default config: %w", marshalErr)
		}
		data = defaultData
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &doc, nil
}

func configDocumentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode {
		*doc = yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	if doc.Content[0].Kind == 0 {
		doc.Content[0].Kind = yaml.MappingNode
	}
	return doc.Content[0]
}

func writeConfigBytes(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	_ = os.Chmod(dir, 0700)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("failed to chmod config to 0600: %w", err)
	}
	return nil
}

func setYAMLPath(root *yaml.Node, key string, value *yaml.Node) error {
	parts := strings.Split(key, ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("empty path segment")
		}
	}
	return setYAMLPathParts(root, parts, value)
}

func setYAMLPathParts(node *yaml.Node, parts []string, value *yaml.Node) error {
	if len(parts) == 0 {
		return nil
	}
	part := parts[0]
	if len(parts) == 1 {
		if idx, ok := configPathIndex(part); ok {
			ensureSequenceNode(node)
			ensureSequenceIndex(node, idx, yaml.MappingNode)
			node.Content[idx] = value
			return nil
		}
		ensureMappingNode(node)
		setMappingValue(node, part, value)
		return nil
	}

	if idx, ok := configPathIndex(part); ok {
		ensureSequenceNode(node)
		ensureSequenceIndex(node, idx, containerKind(parts[1]))
		return setYAMLPathParts(node.Content[idx], parts[1:], value)
	}

	ensureMappingNode(node)
	child := mappingValue(node, part)
	if child == nil {
		child = &yaml.Node{Kind: containerKind(parts[1])}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: part}, child)
	} else if child.Kind == yaml.ScalarNode {
		child.Kind = containerKind(parts[1])
		child.Tag = ""
		child.Value = ""
		child.Content = nil
	}
	return setYAMLPathParts(child, parts[1:], value)
}

func configValueNode(raw string) *yaml.Node {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(trimmed), &doc); err == nil && len(doc.Content) > 0 {
			return doc.Content[0]
		}
	}
	if value, err := strconv.ParseBool(trimmed); err == nil {
		if value {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: trimmed}
	}
	if _, err := strconv.ParseFloat(trimmed, 64); err == nil && strings.Contains(trimmed, ".") {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: trimmed}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: raw}
}

func ensureMappingNode(node *yaml.Node) {
	if node.Kind == yaml.MappingNode {
		return
	}
	node.Kind = yaml.MappingNode
	node.Tag = ""
	node.Value = ""
	node.Content = nil
}

func ensureSequenceNode(node *yaml.Node) {
	if node.Kind == yaml.SequenceNode {
		return
	}
	node.Kind = yaml.SequenceNode
	node.Tag = ""
	node.Value = ""
	node.Content = nil
}

func ensureSequenceIndex(node *yaml.Node, idx int, kind yaml.Kind) {
	for len(node.Content) <= idx {
		node.Content = append(node.Content, &yaml.Node{Kind: kind})
	}
	if node.Content[idx].Kind == 0 {
		node.Content[idx].Kind = kind
	}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func configPathIndex(value string) (int, bool) {
	idx, err := strconv.Atoi(value)
	return idx, err == nil && idx >= 0
}

func containerKind(next string) yaml.Kind {
	if _, ok := configPathIndex(next); ok {
		return yaml.SequenceNode
	}
	return yaml.MappingNode
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
				if cfg.Adapters.GitLab != nil && cfg.Adapters.GitLab.Enabled {
					hasAdapter = true
					if cfg.Adapters.GitLab.Token == "" && os.Getenv("GITLAB_TOKEN") == "" {
						warnings = append(warnings, "GitLab enabled but token not set")
					}
				}
				if cfg.Adapters.Bitbucket != nil && cfg.Adapters.Bitbucket.Enabled {
					hasAdapter = true
					if cfg.Adapters.Bitbucket.Token == "" && os.Getenv("BITBUCKET_TOKEN") == "" {
						warnings = append(warnings, "Bitbucket enabled but token not set")
					}
				}
				if cfg.Adapters.AzureDevOps != nil && cfg.Adapters.AzureDevOps.Enabled {
					hasAdapter = true
					if cfg.Adapters.AzureDevOps.PAT == "" && os.Getenv("AZURE_DEVOPS_PAT") == "" {
						warnings = append(warnings, "Azure DevOps enabled but pat not set")
					}
				}
				if cfg.Adapters.Jira != nil && cfg.Adapters.Jira.Enabled {
					hasAdapter = true
					if cfg.Adapters.Jira.APIToken == "" && os.Getenv("JIRA_API_TOKEN") == "" {
						warnings = append(warnings, "Jira enabled but api_token not set")
					}
				}
				if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled {
					hasAdapter = true
					if cfg.Adapters.Asana.AccessToken == "" && os.Getenv("ASANA_ACCESS_TOKEN") == "" {
						warnings = append(warnings, "Asana enabled but access_token not set")
					}
				}
				if cfg.Adapters.Plane != nil && cfg.Adapters.Plane.Enabled {
					hasAdapter = true
					if cfg.Adapters.Plane.APIKey == "" && os.Getenv("PLANE_API_KEY") == "" {
						warnings = append(warnings, "Plane enabled but api_key not set")
					}
				}
				if cfg.Adapters.Discord != nil && cfg.Adapters.Discord.Enabled {
					hasAdapter = true
					if cfg.Adapters.Discord.BotToken == "" && os.Getenv("DISCORD_BOT_TOKEN") == "" {
						warnings = append(warnings, "Discord enabled but bot_token not set")
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
