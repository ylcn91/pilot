// Replay commands (TASK-21)
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/replay"
)

func newReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay and debug execution recordings",
		Long:  `View, replay, and analyze execution recordings for debugging and improvement.`,
	}

	cmd.AddCommand(
		newReplayListCmd(),
		newReplayShowCmd(),
		newReplayPlayCmd(),
		newReplayAnalyzeCmd(),
		newReplayExportCmd(),
		newReplayDeleteCmd(),
	)

	return cmd
}

func newReplayListCmd() *cobra.Command {
	var (
		limit   int
		project string
		status  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List execution recordings",
		RunE: func(cmd *cobra.Command, args []string) error {
			recordingsPath := replay.DefaultRecordingsPath()

			filter := &replay.RecordingFilter{
				Limit:       limit,
				ProjectPath: project,
				Status:      status,
			}

			recordings, err := replay.ListRecordings(recordingsPath, filter)
			if err != nil {
				return fmt.Errorf("failed to list recordings: %w", err)
			}

			if len(recordings) == 0 {
				fmt.Println("No recordings found.")
				fmt.Println()
				fmt.Println("💡 Recordings are created automatically when you run tasks.")
				return nil
			}

			fmt.Println("📹 Execution Recordings")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			for _, rec := range recordings {
				statusIcon := "✅"
				switch rec.Status {
				case "failed":
					statusIcon = "❌"
				case "cancelled":
					statusIcon = "⚠️"
				}

				fmt.Printf("%s %s\n", statusIcon, rec.ID)
				fmt.Printf("   Task:     %s\n", rec.TaskID)
				fmt.Printf("   Duration: %s | Events: %d\n", rec.Duration.Round(time.Second), rec.EventCount)
				fmt.Printf("   Started:  %s\n", rec.StartTime.Format("2006-01-02 15:04:05"))
				fmt.Println()
			}

			fmt.Printf("Showing %d recording(s)\n", len(recordings))
			fmt.Println()
			fmt.Println("💡 Use 'pilot replay show <id>' for details")
			fmt.Println("   Use 'pilot replay play <id>' to replay")

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum recordings to show")
	cmd.Flags().StringVar(&project, "project", "", "Filter by project path")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (completed, failed, cancelled)")

	return cmd
}

func newReplayShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <recording-id>",
		Short: "Show recording details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordingID := args[0]
			recordingsPath := replay.DefaultRecordingsPath()

			recording, err := replay.LoadRecording(recordingsPath, recordingID)
			if err != nil {
				return fmt.Errorf("failed to load recording: %w", err)
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("📹 RECORDING: %s\n", recording.ID)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			statusIcon := "✅"
			switch recording.Status {
			case "failed":
				statusIcon = "❌"
			case "cancelled":
				statusIcon = "⚠️"
			}

			fmt.Printf("Status:   %s %s\n", statusIcon, recording.Status)
			fmt.Printf("Task:     %s\n", recording.TaskID)
			fmt.Printf("Project:  %s\n", recording.ProjectPath)
			fmt.Printf("Duration: %s\n", recording.Duration.Round(time.Second))
			fmt.Printf("Events:   %d\n", recording.EventCount)
			fmt.Printf("Started:  %s\n", recording.StartTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("Ended:    %s\n", recording.EndTime.Format("2006-01-02 15:04:05"))
			fmt.Println()

			if recording.Metadata != nil {
				fmt.Println("METADATA")
				fmt.Println("───────────────────────────────────────")
				if recording.Metadata.Branch != "" {
					fmt.Printf("  Branch:    %s\n", recording.Metadata.Branch)
				}
				if recording.Metadata.CommitSHA != "" {
					fmt.Printf("  Commit:    %s\n", recording.Metadata.CommitSHA)
				}
				if recording.Metadata.PRUrl != "" {
					fmt.Printf("  PR:        %s\n", recording.Metadata.PRUrl)
				}
				if recording.Metadata.ModelName != "" {
					fmt.Printf("  Model:     %s\n", recording.Metadata.ModelName)
				}
				fmt.Printf("  Navigator: %v\n", recording.Metadata.HasNavigator)
				fmt.Println()
			}

			if recording.TokenUsage != nil {
				fmt.Println("TOKEN USAGE")
				fmt.Println("───────────────────────────────────────")
				fmt.Printf("  Input:    %d tokens\n", recording.TokenUsage.InputTokens)
				fmt.Printf("  Output:   %d tokens\n", recording.TokenUsage.OutputTokens)
				fmt.Printf("  Total:    %d tokens\n", recording.TokenUsage.TotalTokens)
				fmt.Printf("  Cost:     $%.4f\n", recording.TokenUsage.EstimatedCostUSD)
				fmt.Println()
			}

			if len(recording.PhaseTimings) > 0 {
				fmt.Println("PHASE TIMINGS")
				fmt.Println("───────────────────────────────────────")
				for _, pt := range recording.PhaseTimings {
					pct := float64(pt.Duration) / float64(recording.Duration) * 100
					fmt.Printf("  %-12s %8s (%5.1f%%)\n", pt.Phase+":", pt.Duration.Round(time.Second), pct)
				}
				fmt.Println()
			}

			fmt.Println("FILES")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("  Stream:   %s\n", recording.StreamPath)
			fmt.Printf("  Summary:  %s\n", recording.SummaryPath)
			fmt.Println()

			fmt.Println("💡 Use 'pilot replay play " + recording.ID + "' to replay")
			fmt.Println("   Use 'pilot replay analyze " + recording.ID + "' for detailed analysis")

			return nil
		},
	}

	return cmd
}

func newReplayAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze <recording-id>",
		Short: "Analyze an execution recording",
		Long:  `Generate detailed analysis of token usage, phase timing, tool usage, and errors.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordingID := args[0]
			recordingsPath := replay.DefaultRecordingsPath()

			recording, err := replay.LoadRecording(recordingsPath, recordingID)
			if err != nil {
				return fmt.Errorf("failed to load recording: %w", err)
			}

			analyzer, err := replay.NewAnalyzer(recording)
			if err != nil {
				return fmt.Errorf("failed to create analyzer: %w", err)
			}

			report, err := analyzer.Analyze()
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			fmt.Print(replay.FormatReport(report))

			return nil
		},
	}

	return cmd
}

func newReplayDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <recording-id>",
		Short: "Delete a recording",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordingID := args[0]
			recordingsPath := replay.DefaultRecordingsPath()

			// Verify recording exists
			_, err := replay.LoadRecording(recordingsPath, recordingID)
			if err != nil {
				return fmt.Errorf("recording not found: %w", err)
			}

			if !force {
				fmt.Printf("Delete recording %s? [y/N]: ", recordingID)
				var input string
				_, _ = fmt.Scanln(&input)
				if strings.ToLower(input) != "y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			if err := replay.DeleteRecording(recordingsPath, recordingID); err != nil {
				return fmt.Errorf("failed to delete: %w", err)
			}

			fmt.Printf("✅ Deleted recording: %s\n", recordingID)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Delete without confirmation")

	return cmd
}
