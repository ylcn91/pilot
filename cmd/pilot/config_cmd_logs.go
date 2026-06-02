package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/replay"
)

// newLogsCmd creates the logs command for viewing task execution logs
func newLogsCmd() *cobra.Command {
	var (
		limit   int
		follow  bool
		verbose bool
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "logs [task-id]",
		Short: "View task execution logs",
		Long: `View logs from task executions.

Without arguments, shows recent task logs.
With a task ID, shows detailed logs for that specific task.

Examples:
  pilot logs              # Show recent task logs
  pilot logs TASK-12345   # Show logs for specific task
  pilot logs GH-15        # Show logs for GitHub issue task
  pilot logs --limit 20   # Show last 20 tasks`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// If task ID provided, show specific task logs
			if len(args) > 0 {
				return showTaskLogs(args[0], cfg, verbose, jsonOut)
			}

			// Otherwise, show recent logs
			return showRecentLogs(cfg, limit, jsonOut)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Number of recent tasks to show")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (not implemented)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func showTaskLogs(taskID string, cfg *config.Config, verbose, jsonOut bool) error {
	// Try to find recording by task ID
	recordingsPath := replay.DefaultRecordingsPath()

	// List all recordings and find matching task
	recordings, err := replay.ListRecordings(recordingsPath, &replay.RecordingFilter{Limit: 100})
	if err != nil {
		return fmt.Errorf("failed to list recordings: %w", err)
	}

	var matchingRec *replay.RecordingSummary
	for _, rec := range recordings {
		if rec.TaskID == taskID || rec.ID == taskID || strings.Contains(rec.TaskID, taskID) {
			matchingRec = rec
			break
		}
	}

	if matchingRec == nil {
		return fmt.Errorf("no logs found for task: %s", taskID)
	}

	// Load full recording
	recording, err := replay.LoadRecording(recordingsPath, matchingRec.ID)
	if err != nil {
		return fmt.Errorf("failed to load recording: %w", err)
	}

	if jsonOut {
		data, err := json.MarshalIndent(recording, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Display task info
	statusIcon := "+"
	switch recording.Status {
	case "failed":
		statusIcon = "x"
	case "cancelled":
		statusIcon = "!"
	}

	fmt.Printf("Task: %s [%s]\n", recording.TaskID, statusIcon)
	fmt.Printf("Status: %s\n", recording.Status)
	fmt.Printf("Duration: %s\n", recording.Duration)
	fmt.Printf("Started: %s\n", recording.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Println()

	if recording.Metadata != nil {
		if recording.Metadata.Branch != "" {
			fmt.Printf("Branch: %s\n", recording.Metadata.Branch)
		}
		if recording.Metadata.CommitSHA != "" {
			fmt.Printf("Commit: %s\n", recording.Metadata.CommitSHA)
		}
		if recording.Metadata.PRUrl != "" {
			fmt.Printf("PR: %s\n", recording.Metadata.PRUrl)
		}
		fmt.Println()
	}

	if verbose {
		// Load and show events
		events, err := replay.LoadStreamEvents(recording)
		if err != nil {
			return fmt.Errorf("failed to load events: %w", err)
		}

		fmt.Printf("Events (%d):\n", len(events))
		fmt.Println(strings.Repeat("-", 50))

		for i, event := range events {
			formatted := replay.FormatEvent(event, true)
			fmt.Printf("[%d] %s\n", i+1, formatted)
		}
	}

	return nil
}

func showRecentLogs(cfg *config.Config, limit int, jsonOut bool) error {
	recordingsPath := replay.DefaultRecordingsPath()

	recordings, err := replay.ListRecordings(recordingsPath, &replay.RecordingFilter{Limit: limit})
	if err != nil {
		return fmt.Errorf("failed to list recordings: %w", err)
	}

	if len(recordings) == 0 {
		fmt.Println("No task logs found.")
		return nil
	}

	if jsonOut {
		data, err := json.MarshalIndent(recordings, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Recent Tasks (%d):\n", len(recordings))
	fmt.Println()

	for _, rec := range recordings {
		statusIcon := "+"
		switch rec.Status {
		case "failed":
			statusIcon = "x"
		case "cancelled":
			statusIcon = "!"
		}

		fmt.Printf("  [%s] %-20s %8s  %s\n",
			statusIcon,
			rec.TaskID,
			rec.Duration.Round(1),
			rec.StartTime.Format("Jan 02 15:04"))
	}

	fmt.Println()
	fmt.Println("Use 'pilot logs <task-id>' for details")

	return nil
}
