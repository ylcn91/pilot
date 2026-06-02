package main

import (
	"fmt"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

// runTaskDryRun handles the --dry-run path for the task command. It prints what
// would execute and returns the error to propagate from RunE (nil on success).
func runTaskDryRun(task *executor.Task, projectPath string) error {
	// GH-2286: load config to respect executor.type setting
	dryRunCfgPath := cfgFile
	if dryRunCfgPath == "" {
		dryRunCfgPath = config.DefaultConfigPath()
	}
	dryRunCfg, cfgErr := config.Load(dryRunCfgPath)
	if cfgErr != nil {
		return fmt.Errorf("failed to load config for dry-run: %w", cfgErr)
	}
	dryRunBackendCfg := dryRunCfg.Executor
	if dryRunBackendCfg == nil {
		dryRunBackendCfg = executor.DefaultBackendConfig()
	}

	fmt.Println("🧪 DRY RUN - showing what would execute:")
	fmt.Println()
	fmt.Printf("Command: %s -p \"<prompt>\" --verbose --output-format stream-json\n", dryRunBackendCfg.Type)
	fmt.Println("Working directory:", projectPath)
	fmt.Println()
	fmt.Println("Prompt:")
	fmt.Println("─────────────────────────────────────")
	// Build actual prompt using a runner with config
	runner, runnerErr := executor.NewRunnerWithConfig(dryRunBackendCfg)
	if runnerErr != nil {
		return fmt.Errorf("failed to create runner for dry-run: %w", runnerErr)
	}
	// TASK-286 / GH-3027: harmless on the dry-run path (no gh calls),
	// but kept for consistency so the wiring is uniform across sites.
	runner.SetRepoAllowlist(newConfigRepoAllowlist(dryRunCfg))
	prompt := runner.BuildPrompt(task, task.ProjectPath)
	fmt.Println(prompt)
	fmt.Println("─────────────────────────────────────")
	return nil
}
