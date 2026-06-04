package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/executor"
)

// executeTaskWithProgress runs the task through the runner with a progress
// display, writes the optional result JSON, builds and prints the execution
// report, and emits the completion/failure alert events. It returns the error
// to propagate from RunE (nil on success), reproducing the original control
// flow including the early return on execution failure.
func executeTaskWithProgress(
	ctx context.Context,
	runner *executor.Runner,
	task *executor.Task,
	taskDesc, projectPath string,
	verbose bool,
	resultJSON string,
	alertsEngine *alerts.Engine,
) error {
	// Create progress display (disabled in verbose mode - show raw JSON instead)
	progress := executor.NewProgressDisplay(task.ID, taskDesc, !verbose)

	// Suppress slog progress output when visual display is active
	runner.SuppressProgressLogs(!verbose)

	// Track Navigator mode detection
	var detectedNavMode string

	// Set up progress callback
	runner.OnProgress(func(taskID, phase string, pct int, message string) {
		// Detect Navigator mode from phase names
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

		if verbose {
			// Verbose mode: simple line output
			timestamp := time.Now().Format("15:04:05")
			if message != "" {
				fmt.Printf("   [%s] %s (%d%%): %s\n", timestamp, phase, pct, message)
			}
		} else {
			// Normal mode: visual progress display
			progress.Update(phase, pct, message)
		}

		// Send progress event to alerts engine
		if alertsEngine != nil {
			alertsEngine.ProcessEvent(alerts.Event{
				Type:      alerts.EventTypeTaskProgress,
				TaskID:    taskID,
				TaskTitle: taskDesc,
				Project:   projectPath,
				Phase:     phase,
				Progress:  pct,
				Timestamp: time.Now(),
			})
		}
	})

	backendName := "active backend"
	if backend := runner.GetBackend(); backend != nil {
		backendName = backend.Name()
	}
	fmt.Printf("⏳ Executing task with %s...\n", backendName)
	if verbose {
		fmt.Println("   (streaming raw JSON)")
	}
	fmt.Println()

	// Start progress display with Navigator check
	progress.StartWithNavigatorCheck(projectPath)

	// Execute the task
	result, err := runner.Execute(ctx, task)
	if err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	// Write result as JSON if --result-json flag is set
	if resultJSON != "" {
		data, jsonErr := json.MarshalIndent(result, "", "  ")
		if jsonErr != nil {
			fmt.Printf("   ⚠️  Failed to marshal result JSON: %v\n", jsonErr)
		} else if writeErr := os.WriteFile(resultJSON, data, 0644); writeErr != nil {
			fmt.Printf("   ⚠️  Failed to write result JSON to %s: %v\n", resultJSON, writeErr)
		}
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

	// Finish progress display with comprehensive report
	progress.FinishWithReport(report)

	// Send alerts based on result
	if result.Success {
		if result.PRUrl == "" {
			fmt.Println("   ⚠️  PR not created (check gh auth status)")
		}

		// Send task completed event to alerts engine
		if alertsEngine != nil {
			alertsEngine.ProcessEvent(alerts.Event{
				Type:      alerts.EventTypeTaskCompleted,
				TaskID:    task.ID,
				TaskTitle: taskDesc,
				Project:   projectPath,
				Timestamp: time.Now(),
				Metadata: map[string]string{
					"duration":   result.Duration.String(),
					"pr_url":     result.PRUrl,
					"commit_sha": result.CommitSHA,
				},
			})
		}
	} else {
		// Send task failed event to alerts engine
		if alertsEngine != nil {
			alertsEngine.ProcessEvent(alerts.Event{
				Type:      alerts.EventTypeTaskFailed,
				TaskID:    task.ID,
				TaskTitle: taskDesc,
				Project:   projectPath,
				Error:     result.Error,
				Timestamp: time.Now(),
				Metadata: map[string]string{
					"duration": result.Duration.String(),
				},
			})
			// Give time for alert to be sent before exiting
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil
}
