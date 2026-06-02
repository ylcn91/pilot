package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

func interactiveNewTask(cfg *config.Config) error {
	fmt.Println()
	fmt.Println(titleStyle.Render("  New Task"))
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Get task description
	fmt.Print("  Describe your task: ")
	taskDesc, _ := reader.ReadString('\n')
	taskDesc = strings.TrimSpace(taskDesc)

	if taskDesc == "" {
		fmt.Println("  Cancelled.")
		return nil
	}

	// Get project path
	projectPath := ""
	if defaultProj := cfg.GetDefaultProject(); defaultProj != nil {
		projectPath = defaultProj.Path
	} else {
		cwd, _ := os.Getwd()
		projectPath = cwd
	}

	// Confirm
	fmt.Println()
	fmt.Printf("  Task:    %s\n", taskDesc)
	fmt.Printf("  Project: %s\n", projectPath)
	fmt.Println()
	fmt.Print("  Execute? [Y/n]: ")

	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))

	if confirm != "" && confirm != "y" && confirm != "yes" {
		fmt.Println("  Cancelled.")
		return nil
	}

	// Execute task
	taskID := fmt.Sprintf("TASK-%d", time.Now().Unix()%100000)
	branchName := fmt.Sprintf("pilot/%s", taskID)

	task := &executor.Task{
		ID:          taskID,
		Title:       taskDesc,
		Description: taskDesc,
		ProjectPath: projectPath,
		Branch:      branchName,
	}

	// GH-2286: use executor config from config.yaml instead of hardcoded defaults
	backendCfg := cfg.Executor
	if backendCfg == nil {
		backendCfg = executor.DefaultBackendConfig()
	}
	runner, err := executor.NewRunnerWithConfig(backendCfg)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}
	// TASK-286 / GH-3027: refuse sub-issue creation on unmanaged repos.
	runner.SetRepoAllowlist(newConfigRepoAllowlist(cfg))
	progress := executor.NewProgressDisplay(task.ID, taskDesc, true)

	// Suppress slog progress output when visual display is active
	runner.SuppressProgressLogs(true)

	// Track Navigator mode
	var detectedNavMode string

	runner.OnProgress(func(taskID, phase string, pct int, message string) {
		// Detect Navigator mode
		switch phase {
		case "Navigator", "Loop Mode", "Task Mode":
			progress.SetNavigator(true, phase)
			detectedNavMode = phase
		case "Research", "Implement", "Verify":
			if detectedNavMode == "" {
				detectedNavMode = "nav-task"
			}
			progress.SetNavigator(true, detectedNavMode)
		}
		progress.Update(phase, pct, message)
	})

	fmt.Println()
	fmt.Printf("  Executing task with %s...\n", backendCfg.Type)
	fmt.Println()

	// Start with Navigator check
	progress.StartWithNavigatorCheck(projectPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\n  Cancelling task...")
		cancel()
	}()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		progress.Finish(false, err.Error())
		return err
	}

	// Build execution report
	report := &executor.ExecutionReport{
		TaskID:           result.TaskID,
		TaskTitle:        taskDesc,
		Success:          result.Success,
		Duration:         result.Duration,
		Branch:           task.Branch,
		CommitSHA:        result.CommitSHA,
		PRUrl:            result.PRUrl,
		HasNavigator:     detectedNavMode != "",
		NavMode:          detectedNavMode,
		TokensInput:      result.TokensInput,
		TokensOutput:     result.TokensOutput,
		EstimatedCostUSD: result.EstimatedCostUSD,
		ModelName:        result.ModelName,
		ErrorMessage:     result.Error,
	}

	// Finish with comprehensive report
	progress.FinishWithReport(report)

	fmt.Println()
	fmt.Print("  Press Enter to continue...")
	_, _ = reader.ReadString('\n')

	return nil
}
