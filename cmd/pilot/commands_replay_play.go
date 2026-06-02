package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/replay"
)

func newReplayPlayCmd() *cobra.Command {
	var (
		startAt     int
		stopAt      int
		verbose     bool
		interactive bool
		speed       float64
		filterTools bool
		filterText  bool
		filterAll   bool
	)

	cmd := &cobra.Command{
		Use:   "play <recording-id>",
		Short: "Replay an execution recording",
		Long: `Replay an execution recording with an interactive TUI viewer.

The interactive viewer supports:
  - Play/pause with spacebar
  - Speed control (1-4 keys for 0.5x, 1x, 2x, 4x)
  - Event filtering (t=tools, x=text, r=results, s=system, e=errors)
  - Navigation with arrow keys or j/k
  - Jump to start (g) or end (G)

Examples:
  pilot replay play TG-1234567890              # Interactive viewer
  pilot replay play TG-1234567890 --no-tui     # Simple output mode
  pilot replay play TG-1234567890 --start 50   # Start from event 50
  pilot replay play TG-1234567890 --verbose    # Show all details`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordingID := args[0]
			recordingsPath := replay.DefaultRecordingsPath()

			recording, err := replay.LoadRecording(recordingsPath, recordingID)
			if err != nil {
				return fmt.Errorf("failed to load recording: %w", err)
			}

			// Use interactive viewer by default if terminal supports it
			if interactive && replay.CheckTerminalSupport() {
				filter := replay.DefaultEventFilter()
				if filterTools && !filterAll {
					filter = replay.EventFilter{ShowTools: true}
				}
				if filterText && !filterAll {
					filter.ShowText = true
				}

				return replay.RunViewerWithOptions(recording, startAt, filter)
			}

			// Fallback to simple output mode
			options := &replay.ReplayOptions{
				StartAt:     startAt,
				StopAt:      stopAt,
				Speed:       speed,
				ShowTools:   true,
				ShowText:    true,
				ShowResults: verbose,
				Verbose:     verbose,
			}

			player, err := replay.NewPlayer(recording, options)
			if err != nil {
				return fmt.Errorf("failed to create player: %w", err)
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("▶️  REPLAYING: %s\n", recording.ID)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("Task: %s | Events: %d | Duration: %s\n",
				recording.TaskID, recording.EventCount, recording.Duration.Round(time.Second))
			if speed > 0 {
				fmt.Printf("Speed: %.1fx\n", speed)
			}
			fmt.Println()

			// Play with callback
			player.OnEvent(func(event *replay.StreamEvent, index, total int) error {
				formatted := replay.FormatEvent(event, verbose)
				fmt.Printf("[%d/%d] %s\n", index+1, total, formatted)
				return nil
			})

			ctx := context.Background()
			if err := player.Play(ctx); err != nil {
				return fmt.Errorf("replay failed: %w", err)
			}

			fmt.Println()
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("⏹️  REPLAY COMPLETE")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			return nil
		},
	}

	cmd.Flags().IntVar(&startAt, "start", 0, "Start from event sequence number")
	cmd.Flags().IntVar(&stopAt, "stop", 0, "Stop at event sequence number (0 = end)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show all event details")
	cmd.Flags().BoolVar(&interactive, "tui", true, "Use interactive TUI viewer")
	cmd.Flags().Float64Var(&speed, "speed", 0, "Playback speed (0 = instant, 1 = real-time, 2 = 2x, etc)")
	cmd.Flags().BoolVar(&filterTools, "tools-only", false, "Show only tool calls")
	cmd.Flags().BoolVar(&filterText, "text-only", false, "Show only text events")
	cmd.Flags().BoolVar(&filterAll, "all", true, "Show all event types")

	return cmd
}

func newReplayExportCmd() *cobra.Command {
	var (
		format       string
		output       string
		withAnalysis bool
	)

	cmd := &cobra.Command{
		Use:   "export <recording-id>",
		Short: "Export a recording for sharing",
		Long: `Export a recording to HTML, JSON, or Markdown format.

HTML reports include visual charts for phase timing, token breakdown,
and tool usage when --with-analysis is enabled.

Examples:
  pilot replay export TG-1234567890                    # Basic HTML
  pilot replay export TG-1234567890 --with-analysis    # Full report with charts
  pilot replay export TG-1234567890 --format json
  pilot replay export TG-1234567890 --format markdown
  pilot replay export TG-1234567890 --output report.html`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordingID := args[0]
			recordingsPath := replay.DefaultRecordingsPath()

			recording, err := replay.LoadRecording(recordingsPath, recordingID)
			if err != nil {
				return fmt.Errorf("failed to load recording: %w", err)
			}

			events, err := replay.LoadStreamEvents(recording)
			if err != nil {
				return fmt.Errorf("failed to load events: %w", err)
			}

			// Generate analysis if requested
			var report *replay.AnalysisReport
			if withAnalysis || format == "markdown" {
				analyzer, err := replay.NewAnalyzer(recording)
				if err != nil {
					return fmt.Errorf("failed to create analyzer: %w", err)
				}
				report, err = analyzer.Analyze()
				if err != nil {
					return fmt.Errorf("analysis failed: %w", err)
				}
			}

			var content []byte
			var ext string

			switch format {
			case "html":
				ext = "html"
				if withAnalysis && report != nil {
					html, err := replay.ExportHTMLReport(recording, events, report)
					if err != nil {
						return fmt.Errorf("failed to export HTML report: %w", err)
					}
					content = []byte(html)
				} else {
					html, err := replay.ExportToHTML(recording, events)
					if err != nil {
						return fmt.Errorf("failed to export HTML: %w", err)
					}
					content = []byte(html)
				}
			case "json":
				ext = "json"
				content, err = replay.ExportToJSON(recording, events)
				if err != nil {
					return fmt.Errorf("failed to export JSON: %w", err)
				}
			case "markdown", "md":
				ext = "md"
				md, err := replay.ExportToMarkdown(recording, events, report)
				if err != nil {
					return fmt.Errorf("failed to export Markdown: %w", err)
				}
				content = []byte(md)
			default:
				return fmt.Errorf("unsupported format: %s (use html, json, or markdown)", format)
			}

			// Determine output path
			if output == "" {
				output = fmt.Sprintf("%s.%s", recordingID, ext)
			}

			if err := os.WriteFile(output, content, 0644); err != nil {
				return fmt.Errorf("failed to write file: %w", err)
			}

			fmt.Printf("✅ Exported to: %s\n", output)
			fmt.Printf("   Format: %s | Size: %d bytes\n", format, len(content))
			if withAnalysis {
				fmt.Println("   Analysis: ✓ included")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "html", "Export format (html, json, markdown)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path")
	cmd.Flags().BoolVar(&withAnalysis, "with-analysis", false, "Include detailed analysis in export")

	return cmd
}
