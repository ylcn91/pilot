package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

// killExistingTelegramBot finds and kills any running pilot process with Telegram enabled
func killExistingTelegramBot() error {
	currentPID := os.Getpid()

	// Find processes matching "pilot start" or "pilot telegram" (for backward compatibility)
	patterns := []string{"pilot start", "pilot telegram"}
	for _, pattern := range patterns {
		out, err := exec.Command("pgrep", "-f", pattern).Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				continue // No process found
			}
			// pgrep not available, try ps-based approach
			return killExistingBotPS(currentPID, pattern)
		}

		pids := strings.Fields(strings.TrimSpace(string(out)))
		for _, pidStr := range pids {
			var pid int
			if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
				continue
			}
			if pid == currentPID {
				continue
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			_ = proc.Signal(syscall.SIGTERM)
		}
	}

	return nil
}

// killExistingBotPS uses ps + grep as fallback
func killExistingBotPS(currentPID int, pattern string) error {
	out, err := exec.Command("sh", "-c", fmt.Sprintf("ps aux | grep '%s' | grep -v grep | awk '{print $2}'", pattern)).Output()
	if err != nil {
		return nil
	}

	pids := strings.Fields(strings.TrimSpace(string(out)))
	for _, pidStr := range pids {
		var pid int
		if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
			continue
		}
		if pid == currentPID {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = proc.Signal(syscall.SIGTERM)
	}

	return nil
}

// parseInt64 parses a string to int64
func parseInt64(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}

func newGitHubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub integration commands",
		Long:  `Commands for working with GitHub issues and pull requests.`,
	}

	cmd.AddCommand(newGitHubRunCmd())
	return cmd
}

func newGitHubRunCmd() *cobra.Command {
	var projectPath string
	var dryRun bool
	var verbose bool
	var repo string
	var teamID string     // GH-635: team project access scoping
	var teamMember string // GH-635: member email for access scoping

	cmd := &cobra.Command{
		Use:   "run <issue-number>",
		Short: "Run a GitHub issue as a Pilot task",
		Long: `Fetch a GitHub issue and execute it as a Pilot task.

PRs are always created to enable autopilot workflow.

Examples:
  pilot github run 8
  pilot github run 8 --repo owner/repo
  pilot github run 8 --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			issueNum, err := parseInt64(args[0])
			if err != nil {
				return fmt.Errorf("invalid issue number: %s", args[0])
			}

			// Load config
			// Resolve config path
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Apply team flag overrides (GH-635)
			applyTeamOverrides(cfg, cmd, teamID, teamMember)

			// Check GitHub is configured
			if cfg.Adapters == nil || cfg.Adapters.GitHub == nil || !cfg.Adapters.GitHub.Enabled {
				return fmt.Errorf("GitHub adapter not enabled. Run 'pilot setup' or edit ~/.pilot/config.yaml")
			}

			token := cfg.Adapters.GitHub.Token
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			if token == "" {
				return fmt.Errorf("GitHub token not configured. Set GITHUB_TOKEN env or add to config")
			}

			// Determine repo
			if repo == "" {
				repo = cfg.Adapters.GitHub.Repo
			}
			if repo == "" {
				return fmt.Errorf("no repository specified. Use --repo owner/repo or set in config")
			}

			parts := strings.Split(repo, "/")
			if len(parts) != 2 {
				return fmt.Errorf("invalid repo format. Use owner/repo")
			}
			owner, repoName := parts[0], parts[1]

			// Resolve project path
			if projectPath == "" {
				// Try to find project by repo
				for _, p := range cfg.Projects {
					if p.GitHub != nil && p.GitHub.Owner == owner && p.GitHub.Repo == repoName {
						projectPath = p.Path
						break
					}
				}
				if projectPath == "" {
					cwd, _ := os.Getwd()
					projectPath = cwd
				}
			}

			// Fetch issue from GitHub
			client := github.NewClient(token)
			ctx := context.Background()

			fmt.Printf("📥 Fetching issue #%d from %s...\n", issueNum, repo)
			issue, err := client.GetIssue(ctx, owner, repoName, int(issueNum))
			if err != nil {
				return fmt.Errorf("failed to fetch issue: %w", err)
			}

			banner.Print()

			taskID := fmt.Sprintf("GH-%d", issueNum)
			branchName := fmt.Sprintf("pilot/%s", taskID)

			// Check for Navigator
			hasNavigator := false
			if _, err := os.Stat(projectPath + "/.agent"); err == nil {
				hasNavigator = true
			}

			fmt.Println("🚀 Pilot GitHub Task Execution")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("   Issue:     #%d\n", issue.Number)
			fmt.Printf("   Title:     %s\n", issue.Title)
			fmt.Printf("   Task ID:   %s\n", taskID)
			fmt.Printf("   Project:   %s\n", projectPath)
			fmt.Printf("   Branch:    %s\n", branchName)
			fmt.Printf("   Create PR: ✓ always enabled\n")
			if hasNavigator {
				fmt.Printf("   Navigator: ✓ enabled\n")
			}
			fmt.Println()
			fmt.Println("📋 Issue Body:")
			fmt.Println("───────────────────────────────────────")
			if issue.Body != "" {
				fmt.Println(issue.Body)
			} else {
				fmt.Println("(no body)")
			}
			fmt.Println("───────────────────────────────────────")
			fmt.Println()

			// Build task description
			taskDesc := fmt.Sprintf("GitHub Issue #%d: %s\n\n%s", issue.Number, issue.Title, issue.Body)

			// Always create branches and PRs - required for autopilot workflow
			task := &executor.Task{
				ID:          taskID,
				Title:       issue.Title,
				Description: taskDesc,
				ProjectPath: projectPath,
				Branch:      branchName,
				Verbose:     verbose,
				CreatePR:    true,
				Labels:      extractGitHubLabelNames(issue), // GH-727: flow labels for complexity classifier
			}

			// Dry run mode
			if dryRun {
				// GH-2286: use executor config from already-loaded cfg
				ghBackendCfg := cfg.Executor
				if ghBackendCfg == nil {
					ghBackendCfg = executor.DefaultBackendConfig()
				}
				fmt.Println("🧪 DRY RUN - showing what would execute:")
				fmt.Println()
				runner, runnerErr := executor.NewRunnerWithConfig(ghBackendCfg)
				if runnerErr != nil {
					return fmt.Errorf("failed to create runner for dry-run: %w", runnerErr)
				}
				// TASK-286 / GH-3027: harmless on the dry-run path (no gh calls).
				runner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))
				prompt := runner.BuildPrompt(task, task.ProjectPath)
				fmt.Println("Prompt:")
				fmt.Println("─────────────────────────────────────")
				fmt.Println(prompt)
				fmt.Println("─────────────────────────────────────")
				return nil
			}

			// Add in-progress label
			fmt.Println("🏷️  Adding in-progress label...")
			if err := client.AddLabels(ctx, owner, repoName, int(issueNum), []string{"pilot-in-progress"}); err != nil {
				logGitHubAPIError("AddLabels", owner, repoName, int(issueNum), err)
			}

			// Execute the task with config (GH-956: enables worktree isolation, decomposer, model routing)
			runner, runnerErr := executor.NewRunnerWithConfig(cfg.Executor)
			if runnerErr != nil {
				return fmt.Errorf("failed to create executor runner: %w", runnerErr)
			}
			// TASK-286 / GH-3027: refuse sub-issue creation on unmanaged repos.
			runner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))

			// GH-962: Clean up orphaned worktree directories from previous crashed executions
			if cfg.Executor != nil && cfg.Executor.UseWorktree {
				if err := executor.CleanupOrphanedWorktrees(ctx, projectPath); err != nil {
					fmt.Printf("🧹 Worktree cleanup completed (%s)\n", err.Error())
				}
			}

			// Team project access checker (GH-635)
			if ghTeamCleanup := wireProjectAccessChecker(runner, cfg); ghTeamCleanup != nil {
				defer ghTeamCleanup()
				fmt.Printf("   Team:      ✓ project access scoping enabled\n")
			}

			fmt.Println()
			fmt.Println("⏳ Executing task with Claude Code...")
			fmt.Println()

			result, err := runner.Execute(ctx, task)
			if err != nil {
				// Add failed label
				if labelErr := client.AddLabels(ctx, owner, repoName, int(issueNum), []string{"pilot-failed"}); labelErr != nil {
					logGitHubAPIError("AddLabels", owner, repoName, int(issueNum), labelErr)
				}
				if labelErr := client.RemoveLabel(ctx, owner, repoName, int(issueNum), "pilot-in-progress"); labelErr != nil {
					logGitHubAPIError("RemoveLabel", owner, repoName, int(issueNum), labelErr)
				}

				comment := fmt.Sprintf("❌ Pilot execution failed:\n\n```\n%s\n```", err.Error())
				if _, commentErr := client.AddComment(ctx, owner, repoName, int(issueNum), comment); commentErr != nil {
					logGitHubAPIError("AddComment", owner, repoName, int(issueNum), commentErr)
				}

				return fmt.Errorf("task execution failed: %w", err)
			}

			// Remove in-progress label
			if err := client.RemoveLabel(ctx, owner, repoName, int(issueNum), "pilot-in-progress"); err != nil {
				logGitHubAPIError("RemoveLabel", owner, repoName, int(issueNum), err)
			}

			// Validate deliverables - execution succeeded but did it produce anything?
			// GH-3053: skip for epic-parent results. The parent legitimately
			// produces no commit/PR when sub-issues handle the work; flagging
			// it as failed mislabels the issue and posts a misleading comment.
			if !result.IsEpic && result.CommitSHA == "" && result.PRUrl == "" {
				// No commits and no PR - mark as failed
				if err := client.AddLabels(ctx, owner, repoName, int(issueNum), []string{"pilot-failed"}); err != nil {
					logGitHubAPIError("AddLabels", owner, repoName, int(issueNum), err)
				}

				comment := fmt.Sprintf("⚠️ Pilot execution completed but no changes were made.\n\n**Duration:** %s\n**Branch:** `%s`\n\nNo commits or PR were created. The task may need clarification or manual intervention.",
					result.Duration, branchName)
				if _, err := client.AddComment(ctx, owner, repoName, int(issueNum), comment); err != nil {
					logGitHubAPIError("AddComment", owner, repoName, int(issueNum), err)
				}

				fmt.Println()
				fmt.Println("───────────────────────────────────────")
				fmt.Println("⚠️  Task completed but no changes made")
				fmt.Printf("   Duration: %s\n", result.Duration)
				return fmt.Errorf("execution completed but no commits or PR created")
			}

			// Success with deliverables - keep pilot-in-progress until PR merges
			// GH-1015: pilot-done is now added by autopilot controller after successful merge
			// This prevents false positives where PRs are closed without merging
			// Remove pilot-failed if present (may exist from previous failed attempt)
			_ = client.RemoveLabel(ctx, owner, repoName, int(issueNum), "pilot-failed")

			comment := buildExecutionComment(result, branchName)
			if _, err := client.AddComment(ctx, owner, repoName, int(issueNum), comment); err != nil {
				logGitHubAPIError("AddComment", owner, repoName, int(issueNum), err)
			}

			fmt.Println()
			fmt.Println("───────────────────────────────────────")
			fmt.Println("✅ Task completed successfully!")
			fmt.Printf("   Duration: %s\n", result.Duration)
			if result.PRUrl != "" {
				fmt.Printf("   PR: %s\n", result.PRUrl)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository (owner/repo)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would execute without running")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.Flags().StringVar(&teamID, "team", "", "Team ID or name for project access scoping (overrides config)")
	cmd.Flags().StringVar(&teamMember, "team-member", "", "Member email for team access scoping (overrides config)")

	return cmd
}
