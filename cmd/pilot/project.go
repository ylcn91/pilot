package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage Pilot projects",
		Long:  `Add, list, remove, and configure projects for Pilot.`,
	}

	cmd.AddCommand(
		newProjectListCmd(),
		newProjectAddCmd(),
		newProjectRemoveCmd(),
		newProjectSetDefaultCmd(),
		newProjectShowCmd(),
	)

	return cmd
}

func newProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Projects) == 0 {
				fmt.Println("No projects configured.")
				fmt.Println()
				fmt.Println("Add a project with: pilot project add --name <name>")
				return nil
			}

			fmt.Printf("PROJECTS (%d configured)\n\n", len(cfg.Projects))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintf(w, "  NAME\tPATH\tGITHUB\tBRANCH\tNAV\tDEFAULT\n")

			for _, proj := range cfg.Projects {
				navIcon := ""
				if proj.Navigator {
					navIcon = "*"
				}
				defaultIcon := ""
				if proj.Name == cfg.DefaultProject {
					defaultIcon = "*"
				}
				githubStr := ""
				if proj.GitHub != nil {
					githubStr = fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)
				}
				_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n",
					proj.Name,
					truncatePath(proj.Path, 40),
					githubStr,
					proj.DefaultBranch,
					navIcon,
					defaultIcon,
				)
			}
			_ = w.Flush()

			fmt.Println()
			fmt.Println("Use 'pilot project show <name>' for details")

			return nil
		},
	}
}

func newProjectAddCmd() *cobra.Command {
	var (
		name       string
		path       string
		github     string
		branch     string
		navigator  bool
		setDefault bool
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new project",
		Long: `Add a new project to Pilot configuration.

Auto-detection:
  - If --path is omitted, uses current working directory
  - If --branch is omitted, detects from git remote
  - If --navigator is omitted, checks for .agent/ directory
  - If --github is omitted, parses from git remote origin

Examples:
  pilot project add --name my-app
  pilot project add --name my-app --github owner/repo
  pilot project add -n my-app -p /path/to/project -g owner/repo -b main`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Check for duplicate name
			if cfg.GetProjectByName(name) != nil {
				return fmt.Errorf("project '%s' already exists", name)
			}

			// Use current directory if path not specified
			if path == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current directory: %w", err)
				}
				path = cwd
			}

			// Expand and validate path
			path = expandProjectPath(path)
			info, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("path does not exist: %s", path)
				}
				return fmt.Errorf("failed to access path: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("path is not a directory: %s", path)
			}

			// Check for duplicate path
			if cfg.GetProject(path) != nil {
				return fmt.Errorf("path already configured: %s", path)
			}

			// Auto-detect GitHub if not specified
			var ghConfig *config.ProjectGitHubConfig
			if github != "" {
				parts := strings.SplitN(github, "/", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid GitHub format, expected owner/repo: %s", github)
				}
				ghConfig = &config.ProjectGitHubConfig{
					Owner: parts[0],
					Repo:  parts[1],
				}
			} else {
				// Try to auto-detect from git remote
				ghConfig = detectGitHubFromRemote(path)
			}

			// Auto-detect branch if not specified
			if branch == "" {
				branch = detectDefaultBranch(path)
			}

			// Auto-detect navigator if flag not explicitly set
			if !cmd.Flags().Changed("navigator") {
				navigator = detectNavigator(path)
			}

			// Create project config
			proj := &config.ProjectConfig{
				Name:          name,
				Path:          path,
				Navigator:     navigator,
				DefaultBranch: branch,
				GitHub:        ghConfig,
			}

			// Add to config
			cfg.Projects = append(cfg.Projects, proj)

			// Set as default if requested or if it's the only project
			if setDefault || len(cfg.Projects) == 1 {
				cfg.DefaultProject = name
			}

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			// Print success message
			fmt.Printf("Project added: %s\n", name)
			fmt.Printf("   Path:      %s\n", path)
			if ghConfig != nil {
				fmt.Printf("   GitHub:    %s/%s\n", ghConfig.Owner, ghConfig.Repo)
			}
			if branch != "" {
				fmt.Printf("   Branch:    %s\n", branch)
			}
			navStr := "disabled"
			if navigator {
				navStr = "enabled"
			}
			fmt.Printf("   Navigator: %s\n", navStr)
			fmt.Println()
			fmt.Printf("   Start working: pilot start --project %s\n", name)

			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Project name (required)")
	cmd.Flags().StringVarP(&path, "path", "p", "", "Project path (default: current directory)")
	cmd.Flags().StringVarP(&github, "github", "g", "", "GitHub repo (owner/repo)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Default branch (auto-detected)")
	cmd.Flags().BoolVar(&navigator, "navigator", false, "Enable Navigator (auto-detected)")
	cmd.Flags().BoolVarP(&setDefault, "set-default", "d", false, "Set as default project")

	return cmd
}
