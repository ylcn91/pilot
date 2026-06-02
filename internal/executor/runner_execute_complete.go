package executor

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// executeCompletedEvents emits the task-completed event/webhook, finishes
// recording, syncs the Navigator index + markers, optionally syncs main, and
// stores experiential memory (original lines ~2347-2482). It runs at the tail of
// the success branch and only mutates state.
func (r *Runner) executeCompletedEvents(s *executeState) {
	task := s.task
	ctx := s.ctx
	log := s.log
	executionPath := s.executionPath
	result := s.result
	recorder := s.recorder
	state := s.state
	duration := s.duration

	// GH-1599: Log task completed milestone
	r.saveLogEntry(task.ID, "info", "Task completed successfully")

	// Emit task completed event
	r.emitAlertEvent(AlertEvent{
		Type:      AlertEventTypeTaskCompleted,
		TaskID:    task.ID,
		TaskTitle: task.Title,
		Project:   task.ProjectPath,
		Metadata: map[string]string{
			"duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
			"pr_url":      result.PRUrl,
		},
		Timestamp: time.Now(),
	})

	// Dispatch webhook for task completed
	r.dispatchWebhook(ctx, webhooks.EventTaskCompleted, webhooks.TaskCompletedData{
		TaskID:    task.ID,
		Title:     task.Title,
		Project:   task.ProjectPath,
		Duration:  duration,
		PRCreated: result.PRUrl != "",
		PRURL:     result.PRUrl,
	})

	// Finish recording with completed status
	if recorder != nil {
		recorder.SetCommitSHA(result.CommitSHA)
		recorder.SetModel(result.ModelName)
		recorder.SetNavigator(state.hasNavigator)
		if finErr := recorder.Finish("completed"); finErr != nil {
			log.Warn("Failed to finish recording", slog.Any("error", finErr))
		} else {
			log.Info("Recording saved", slog.String("recording_id", recorder.GetRecordingID()))
		}
	}

	// Sync Navigator index (GH-57) - update DEVELOPMENT-README.md
	if state.hasNavigator {
		if syncErr := r.syncNavigatorIndex(task, "completed", executionPath); syncErr != nil {
			log.Warn("Failed to sync Navigator index", slog.Any("error", syncErr))
		}

		// GH-1063: Archive completed task documentation
		agentPath := filepath.Join(executionPath, ".agent")
		if archiveErr := ArchiveTaskDoc(agentPath, task.ID); archiveErr != nil {
			log.Warn("Failed to archive task documentation", slog.Any("error", archiveErr))
		}

		// GH-1388: Update feature matrix for feature tasks
		if strings.HasPrefix(strings.ToLower(task.Title), "feat(") {
			ver := "unknown"
			if r.config != nil && r.config.Version != "" {
				ver = r.config.Version
			}
			if fmErr := UpdateFeatureMatrix(agentPath, task, ver); fmErr != nil {
				log.Warn("Failed to update feature matrix", slog.Any("error", fmErr))
			}
		}

		// GH-1064: Create context marker for completed task
		marker := &ContextMarker{
			Name:        fmt.Sprintf("task-completed-%s", task.ID),
			Description: fmt.Sprintf("Task completed: %s", task.Title),
			TaskID:      task.ID,
			CurrentFocus: fmt.Sprintf("Completed %s. %d files changed, %d lines added, %d removed. Cost: $%.2f.",
				task.Title, result.FilesChanged, result.LinesAdded, result.LinesRemoved,
				result.EstimatedCostUSD),
		}

		// Add modified files list (GH-1388)
		if len(state.modifiedFiles) > 0 {
			marker.CurrentFocus += fmt.Sprintf(" Modified: %s.", strings.Join(state.modifiedFiles, ", "))
		}

		// Add commit SHA and PR info if available
		if result.CommitSHA != "" {
			marker.Commits = append(marker.Commits, result.CommitSHA)
		}
		if result.PRUrl != "" {
			marker.CurrentFocus += fmt.Sprintf(" PR: %s", result.PRUrl)
		}

		if createMarkerErr := CreateMarker(agentPath, marker); createMarkerErr != nil {
			log.Warn("Failed to create completion marker", slog.Any("error", createMarkerErr))
		} else {
			log.Debug("Created completion context marker", slog.String("marker_path", marker.FilePath))
		}
	}

	// GH-1018: Sync main branch with origin after task completion
	// This prevents local/remote divergence over time
	if r.config != nil && r.config.SyncMainAfterTask {
		if syncErr := r.syncMainBranch(ctx, task.ProjectPath); syncErr != nil {
			log.Warn("Failed to sync main branch", slog.Any("error", syncErr))
		}
	}

	// GH-1065: Store experiential memory after successful task completion
	if r.knowledge != nil {
		projectID := "pilot" // Default fallback
		if task.ProjectPath != "" {
			projectID = filepath.Base(task.ProjectPath)
		}

		// Enrich content with execution metrics
		content := fmt.Sprintf(
			"Completed %s: %s. Modified %d files. Duration: %v. Model: %s.",
			task.ID, task.Title, result.FilesChanged, duration, result.ModelName,
		)
		if result.IntentWarning != "" {
			content += fmt.Sprintf(" Intent warning: %s.", result.IntentWarning)
		}

		// Enrich context with branch, PR URL, and cost
		contextStr := fmt.Sprintf("Branch: %s, Cost: $%.2f",
			task.Branch, result.EstimatedCostUSD)
		if result.PRUrl != "" {
			contextStr += fmt.Sprintf(", PR: %s", result.PRUrl)
		}

		memory := &memory.Memory{
			Type:       memory.MemoryTypeLearning,
			Content:    content,
			Context:    contextStr,
			Confidence: 1.0,
			ProjectID:  projectID,
		}

		if addErr := r.knowledge.AddMemory(memory); addErr != nil {
			log.Warn("Failed to store task completion memory", slog.Any("error", addErr))
		} else {
			log.Debug("Stored task completion memory", slog.String("task_id", task.ID))
		}
	}
}
