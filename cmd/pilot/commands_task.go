package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/banner"
	"github.com/ylcn91/pilot/internal/executor"
)

func newTaskCmd() *cobra.Command {
	var projectPath string
	var dryRun bool
	var verbose bool
	var enableAlerts bool
	var enableBudget bool
	var localMode bool    // GH-2103: problem-solving prompt without PR constraints
	var resultJSON string // Write ExecutionResult as JSON to file
	var teamID string     // GH-635: team project access scoping
	var teamMember string // GH-635: member email for access scoping

	cmd := &cobra.Command{
		Use:   "task [description]",
		Short: "Execute a task using Claude Code",
		Long: `Execute a task using Claude Code with Navigator integration.

PRs are always created to enable autopilot workflow.

Examples:
  pilot task "Add user authentication with JWT"
  pilot task "Fix the login bug in auth.go" --project /path/to/project
  pilot task "Refactor the API handlers" --dry-run
  pilot task "Add index.py with hello world" --verbose
  pilot task "Fix bug" --alerts
  pilot task "Fix bug" --local`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskDesc := args[0]

			// Create context with cancellation on SIGINT
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle Ctrl+C
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("\n\n⚠️  Cancelling task...")
				cancel()
			}()

			banner.Print()

			// Resolve project path
			if projectPath == "" {
				cwd, _ := os.Getwd()
				projectPath = cwd
			}

			// Generate task ID based on timestamp
			taskID := fmt.Sprintf("TASK-%d", time.Now().Unix()%100000)
			branchName := fmt.Sprintf("pilot/%s", taskID)

			// Check for Navigator
			hasNavigator := false
			if _, err := os.Stat(projectPath + "/.agent"); err == nil {
				hasNavigator = true
			}

			fmt.Println("🚀 Pilot Task Execution")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("   Task ID:   %s\n", taskID)
			fmt.Printf("   Project:   %s\n", projectPath)
			if localMode {
				fmt.Printf("   Mode:      local (no git workflow)\n")
			} else {
				fmt.Printf("   Branch:    %s\n", branchName)
				fmt.Printf("   Create PR: ✓ always enabled\n")
			}
			if hasNavigator {
				fmt.Printf("   Navigator: ✓ enabled\n")
			}
			fmt.Println()
			fmt.Println("📋 Task:")
			fmt.Printf("   %s\n", taskDesc)
			fmt.Println("───────────────────────────────────────")
			fmt.Println()

			// Build the task early so we can show prompt in dry-run
			// Always create branches and PRs - required for autopilot workflow
			task := &executor.Task{
				ID:          taskID,
				Title:       taskDesc,
				Description: taskDesc,
				ProjectPath: projectPath,
				Branch:      branchName,
				Verbose:     verbose,
				CreatePR:    true,
				LocalMode:   localMode, // GH-2103
			}

			// Local mode: skip git workflow (no branch/push/PR)
			if localMode {
				task.CreatePR = false
				task.Branch = ""
				task.DirectCommit = false
				task.LocalMode = true
			}

			// Dry run mode - just show what would happen
			if dryRun {
				return runTaskDryRun(task, projectPath)
			}

			// Check budget before task execution if --budget flag is set
			if enableBudget {
				budgetCloser, budgetErr := checkTaskBudget(ctx)
				if budgetCloser != nil {
					defer func() { _ = budgetCloser() }()
				}
				if budgetErr != nil {
					return budgetErr
				}
			}

			// Initialize alerts engine if --alerts flag is set
			var alertsEngine *alerts.Engine
			if enableAlerts {
				engine, alertsErr := setupTaskAlerts(ctx, taskID, taskDesc, projectPath)
				if alertsErr != nil {
					return alertsErr
				}
				alertsEngine = engine
				defer alertsEngine.Stop()
			}

			// Load config and build the executor runner with all task wiring
			runner, cfg, teamCleanup, runnerErr := setupTaskRunner(ctx, cmd, projectPath, teamID, teamMember)
			if runnerErr != nil {
				return runnerErr
			}
			if teamCleanup != nil {
				defer teamCleanup()
			}

			// GH-2146: Initialize learning system for task command
			if learningCloser := wireTaskLearning(runner, cfg); learningCloser != nil {
				defer func() { _ = learningCloser() }()
			}

			// Run the task with progress display and result/alert reporting
			return executeTaskWithProgress(ctx, runner, task, taskDesc, projectPath, verbose, resultJSON, alertsEngine)
		},
	}

	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path (default: current directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be executed without running")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Stream Claude Code output")
	cmd.Flags().BoolVar(&enableAlerts, "alerts", false, "Enable alerts for task execution")
	cmd.Flags().BoolVar(&enableBudget, "budget", false, "Enable budget enforcement for this task")
	cmd.Flags().BoolVar(&localMode, "local", false, "Use problem-solving prompt without PR/Navigator constraints")
	cmd.Flags().StringVar(&resultJSON, "result-json", "", "Write execution result as JSON to file path")
	cmd.Flags().StringVar(&teamID, "team", "", "Team ID or name for project access scoping (overrides config)")
	cmd.Flags().StringVar(&teamMember, "team-member", "", "Member email for team access scoping (overrides config)")

	return cmd
}
