package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/dashboard"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilot"
	"github.com/ylcn91/pilot/internal/upgrade"
)

// checkForUpdates checks for new versions in the background
func checkForUpdates() {
	if quietMode {
		return
	}

	upgrader, err := upgrade.NewUpgrader(version)
	if err != nil {
		return // Silently fail
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := upgrader.CheckVersion(ctx)
	if err != nil {
		return // Silently fail
	}

	if info.UpdateAvail {
		fmt.Println()
		fmt.Printf("✨ Update available: %s → %s\n", info.Current, info.Latest)
		fmt.Println("   Run 'pilot upgrade' to install")
		fmt.Println()
	}
}

// runDashboardMode runs the TUI dashboard with live task updates
func runDashboardMode(p *pilot.Pilot, cfg *config.Config, gwProgram *tea.Program, gwMonitor *executor.Monitor, gwRunner *executor.Runner) error {
	// Suppress slog output to prevent corrupting TUI display (GH-164)
	logging.Suppress()
	p.SuppressProgressLogs(true)

	// GH-2291: Use the pre-built gateway program when adapter pollers are active.
	// gwProgram has the richer model (store, autopilot, project path) and is already
	// referenced by handler_common.go's deps.Program for task start/complete log sends.
	// When no adapter pollers are active, create a basic program for pure webhook mode.
	program := gwProgram
	if program == nil {
		model := dashboard.NewModel(version)
		program = tea.NewProgram(model, tea.WithAltScreen())
	}

	// Set up event bridge: poll task states and send to dashboard
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// GH-2291: collectTasks merges task states from adapter pollers (gwMonitor)
	// and gateway webhook tasks (p.GetTaskStates()) into a single view.
	// convertTaskStatesToDisplay deduplicates by task ID.
	collectTasks := func() []dashboard.TaskDisplay {
		var allStates []*executor.TaskState
		if gwMonitor != nil {
			allStates = append(allStates, gwMonitor.GetAll()...)
		}
		allStates = append(allStates, p.GetTaskStates()...)
		return convertTaskStatesToDisplay(allStates)
	}

	// GH-2291: Wire adapter poller runner progress/token callbacks to the dashboard.
	// These callbacks also update gwMonitor so collectTasks() returns current data.
	if gwRunner != nil && gwMonitor != nil {
		var gwLastUpdate time.Time
		var gwMu sync.Mutex
		gwRunner.AddProgressCallback("dashboard", func(taskID, phase string, progress int, message string) {
			gwMonitor.UpdateProgress(taskID, phase, progress, message)

			gwMu.Lock()
			if time.Since(gwLastUpdate) < 200*time.Millisecond {
				gwMu.Unlock()
				return // Skip — periodic ticker will catch it
			}
			gwLastUpdate = time.Now()
			gwMu.Unlock()

			tasks := collectTasks()
			program.Send(dashboard.UpdateTasks(tasks)())
			logMsg := fmt.Sprintf("[%s] %s: %s (%d%%)", taskID, phase, message, progress)
			program.Send(dashboard.AddLog(logMsg)())
		})

		gwRunner.AddTokenCallback("dashboard", func(taskID string, inputTokens, outputTokens int64, modelName string) {
			program.Send(dashboard.UpdateTokens(int(inputTokens), int(outputTokens), modelName)())
		})
	}

	// Register progress callback on Pilot's orchestrator (gateway webhook tasks)
	// GH-1220: Throttle progress callbacks to 200ms to prevent message flooding
	var lastDashboardUpdate time.Time
	var dashboardMu sync.Mutex
	p.OnProgress(func(taskID, phase string, progress int, message string) {
		dashboardMu.Lock()
		if time.Since(lastDashboardUpdate) < 200*time.Millisecond {
			dashboardMu.Unlock()
			return // Skip — periodic ticker will catch it
		}
		lastDashboardUpdate = time.Now()
		dashboardMu.Unlock()

		tasks := collectTasks()
		program.Send(dashboard.UpdateTasks(tasks)())

		logMsg := fmt.Sprintf("[%s] %s: %s (%d%%)", taskID, phase, message, progress)
		program.Send(dashboard.AddLog(logMsg)())
	})

	// Register token usage callback for dashboard updates (GH-156 fix)
	p.OnToken("dashboard", func(taskID string, inputTokens, outputTokens int64, modelName string) {
		program.Send(dashboard.UpdateTokens(int(inputTokens), int(outputTokens), modelName)())
	})

	// Periodic refresh to catch any missed updates
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tasks := collectTasks()
				program.Send(dashboard.UpdateTasks(tasks)())
			}
		}
	}()

	// Handle signals for graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
		program.Send(tea.Quit())
	}()

	// Add startup log AFTER program starts (GH-351: Send blocks if called before Run)
	gatewayURL := fmt.Sprintf("http://%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	go func() {
		time.Sleep(100 * time.Millisecond) // Wait for program.Run() to start
		program.Send(dashboard.AddLog(fmt.Sprintf("🚀 Pilot %s started - Gateway: %s", version, gatewayURL))())
	}()

	// Run TUI (blocks until quit)
	_, err := program.Run()
	if err != nil {
		return fmt.Errorf("dashboard error: %w", err)
	}

	// Clean shutdown
	return p.Stop()
}

// convertTaskStatesToDisplay converts executor TaskStates to dashboard TaskDisplay format.
// Maps all 5 states: done, running, queued, pending, failed for state-aware dashboard rendering.
// GH-1220: Added deduplication safety net to prevent duplicate tasks in rendering.
func convertTaskStatesToDisplay(states []*executor.TaskState) []dashboard.TaskDisplay {
	seen := make(map[string]bool)
	var displays []dashboard.TaskDisplay
	for _, state := range states {
		// GH-1220: Skip duplicate task IDs to prevent duplicate panels
		if seen[state.ID] {
			continue
		}
		seen[state.ID] = true

		var status string
		switch state.Status {
		case executor.StatusRunning:
			status = "running"
		case executor.StatusQueued:
			status = "queued"
		case executor.StatusCompleted:
			status = "done"
		case executor.StatusFailed:
			status = "failed"
		default:
			status = "pending"
		}

		var duration string
		if state.StartedAt != nil {
			elapsed := time.Since(*state.StartedAt)
			duration = elapsed.Round(time.Second).String()
		}

		displays = append(displays, dashboard.TaskDisplay{
			ID:          state.ID,
			Title:       state.Title,
			Status:      status,
			Phase:       state.Phase,
			Progress:    state.Progress,
			Duration:    duration,
			IssueURL:    state.IssueURL,
			PRURL:       state.PRUrl,
			ProjectPath: state.ProjectPath,
			ProjectName: state.ProjectName,
		})
	}
	return displays
}
