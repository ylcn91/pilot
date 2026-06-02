package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
)

func newProjectRemoveCmd() *cobra.Command {
	var (
		name  string
		force bool
	)

	cmd := &cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a project",
		Long: `Remove a project from Pilot configuration.

Examples:
  pilot project remove my-app
  pilot project remove --name my-app
  pilot project remove my-app --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get name from positional arg or flag
			if len(args) > 0 {
				name = args[0]
			}
			if name == "" {
				return fmt.Errorf("project name required")
			}

			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Find project
			proj := cfg.GetProjectByName(name)
			if proj == nil {
				return fmt.Errorf("project not found: %s", name)
			}

			// Confirm removal unless --force
			if !force {
				fmt.Printf("Remove project '%s'? [y/N] ", name)
				reader := bufio.NewReader(os.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read response: %w", err)
				}
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			// Remove project
			newProjects := make([]*config.ProjectConfig, 0, len(cfg.Projects)-1)
			for _, p := range cfg.Projects {
				if !strings.EqualFold(p.Name, name) {
					newProjects = append(newProjects, p)
				}
			}
			cfg.Projects = newProjects

			// Clear default if it was the removed project
			if strings.EqualFold(cfg.DefaultProject, name) {
				cfg.DefaultProject = ""
				if len(cfg.Projects) > 0 {
					cfg.DefaultProject = cfg.Projects[0].Name
				}
			}

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("Project removed: %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Project name")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

func newProjectSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <name>",
		Short: "Set the default project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Find project
			proj := cfg.GetProjectByName(name)
			if proj == nil {
				return fmt.Errorf("project not found: %s", name)
			}

			// Set default
			cfg.DefaultProject = proj.Name

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("Default project set: %s\n", proj.Name)
			return nil
		},
	}
}

func newProjectShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show project details",
		Long: `Show details for a project.

Without arguments, shows the default project.

Examples:
  pilot project show my-app
  pilot project show`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			var proj *config.ProjectConfig
			if len(args) > 0 {
				proj = cfg.GetProjectByName(args[0])
				if proj == nil {
					return fmt.Errorf("project not found: %s", args[0])
				}
			} else {
				proj = cfg.GetDefaultProject()
				if proj == nil {
					return fmt.Errorf("no default project configured")
				}
			}

			fmt.Printf("PROJECT: %s\n\n", proj.Name)
			fmt.Printf("  Path:       %s\n", proj.Path)
			if proj.GitHub != nil {
				fmt.Printf("  GitHub:     %s/%s\n", proj.GitHub.Owner, proj.GitHub.Repo)
			}
			if proj.DefaultBranch != "" {
				fmt.Printf("  Branch:     %s\n", proj.DefaultBranch)
			}
			navStr := "disabled"
			if proj.Navigator {
				navStr = "enabled"
			}
			fmt.Printf("  Navigator:  %s\n", navStr)
			defaultStr := "no"
			if proj.Name == cfg.DefaultProject {
				defaultStr = "yes"
			}
			fmt.Printf("  Default:    %s\n", defaultStr)

			return nil
		},
	}
}
