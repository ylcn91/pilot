package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

func newMetricsProjectsCmd() *cobra.Command {
	var (
		days  int
		limit int
	)

	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Show metrics by project",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Build query
			end := time.Now()
			start := end.AddDate(0, 0, -days)
			query := memory.MetricsQuery{
				Start: start,
				End:   end,
			}

			// Get project metrics
			metrics, err := store.GetProjectMetrics(query)
			if err != nil {
				return fmt.Errorf("failed to get project metrics: %w", err)
			}

			if len(metrics) == 0 {
				fmt.Println("No executions found in the specified period.")
				return nil
			}

			// Limit results
			if limit > 0 && len(metrics) > limit {
				metrics = metrics[:limit]
			}

			// Display project breakdown
			fmt.Println()
			fmt.Printf("📁 Project Metrics (Last %d Days)\n", days)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			for _, m := range metrics {
				fmt.Printf("\n📦 %s\n", m.ProjectName)
				fmt.Printf("   Path:     %s\n", shortenPath(m.ProjectPath))
				fmt.Printf("   Tasks:    %d (%.1f%% success)\n", m.ExecutionCount, m.SuccessRate*100)
				fmt.Printf("   Duration: %s\n", formatDuration(m.TotalDurationMs))
				fmt.Printf("   Tokens:   %s\n", formatTokens(m.TotalTokens))
				fmt.Printf("   Cost:     $%.2f\n", m.TotalCostUSD)
				fmt.Printf("   Last:     %s\n", m.LastExecution.Format("2006-01-02 15:04"))
			}

			fmt.Println()
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			return nil
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "Number of days to include")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum projects to show")

	return cmd
}

func newMetricsExportCmd() *cobra.Command {
	var (
		days     int
		projects []string
		format   string
		output   string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export metrics data to JSON or CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Build query
			end := time.Now()
			start := end.AddDate(0, 0, -days)
			query := memory.MetricsQuery{
				Start:    start,
				End:      end,
				Projects: projects,
			}

			// Export data
			data, err := store.ExportMetrics(query)
			if err != nil {
				return fmt.Errorf("failed to export metrics: %w", err)
			}

			if len(data) == 0 {
				fmt.Println("No executions found in the specified period.")
				return nil
			}

			// Determine output destination
			var writer *os.File
			if output == "" || output == "-" {
				writer = os.Stdout
			} else {
				writer, err = os.Create(output)
				if err != nil {
					return fmt.Errorf("failed to create output file: %w", err)
				}
				defer func() { _ = writer.Close() }()
			}

			// Export based on format
			switch format {
			case "json":
				encoder := json.NewEncoder(writer)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(data); err != nil {
					return fmt.Errorf("failed to write JSON: %w", err)
				}

			case "csv":
				csvWriter := csv.NewWriter(writer)
				defer csvWriter.Flush()

				// Write header
				header := []string{
					"id", "task_id", "project_path", "status", "duration_ms",
					"tokens_input", "tokens_output", "tokens_total", "estimated_cost_usd",
					"files_changed", "lines_added", "lines_removed", "model_name",
					"pr_url", "commit_sha", "created_at", "completed_at",
				}
				if err := csvWriter.Write(header); err != nil {
					return fmt.Errorf("failed to write CSV header: %w", err)
				}

				// Write rows
				for _, e := range data {
					completedAt := ""
					if e.CompletedAt != nil {
						completedAt = e.CompletedAt.Format(time.RFC3339)
					}

					row := []string{
						e.ID, e.TaskID, e.ProjectPath, e.Status,
						fmt.Sprintf("%d", e.DurationMs),
						fmt.Sprintf("%d", e.TokensInput),
						fmt.Sprintf("%d", e.TokensOutput),
						fmt.Sprintf("%d", e.TokensTotal),
						fmt.Sprintf("%.6f", e.EstimatedCostUSD),
						fmt.Sprintf("%d", e.FilesChanged),
						fmt.Sprintf("%d", e.LinesAdded),
						fmt.Sprintf("%d", e.LinesRemoved),
						e.ModelName, e.PRUrl, e.CommitSHA,
						e.CreatedAt.Format(time.RFC3339),
						completedAt,
					}
					if err := csvWriter.Write(row); err != nil {
						return fmt.Errorf("failed to write CSV row: %w", err)
					}
				}

			default:
				return fmt.Errorf("unsupported format: %s (use 'json' or 'csv')", format)
			}

			if output != "" && output != "-" {
				fmt.Printf("Exported %d records to %s\n", len(data), output)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "Number of days to include")
	cmd.Flags().StringSliceVar(&projects, "projects", nil, "Filter by project paths")
	cmd.Flags().StringVar(&format, "format", "json", "Output format (json or csv)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (- for stdout)")

	return cmd
}
