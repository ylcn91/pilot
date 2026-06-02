package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

func newMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "View execution metrics and analytics",
		Long:  `View aggregated metrics, daily breakdowns, and export data for analysis.`,
	}

	cmd.AddCommand(
		newMetricsSummaryCmd(),
		newMetricsDailyCmd(),
		newMetricsProjectsCmd(),
		newMetricsExportCmd(),
	)

	return cmd
}

func newMetricsSummaryCmd() *cobra.Command {
	var (
		days     int
		projects []string
	)

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show metrics summary for the last N days",
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

			// Get summary
			summary, err := store.GetMetricsSummary(query)
			if err != nil {
				return fmt.Errorf("failed to get metrics: %w", err)
			}

			// Display summary
			fmt.Println()
			fmt.Printf("📊 Pilot Metrics Summary (Last %d Days)\n", days)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			// Execution stats
			fmt.Println("📈 Executions")
			fmt.Printf("   Total:    %d\n", summary.TotalExecutions)
			fmt.Printf("   Success:  %d (%.1f%%)\n", summary.SuccessCount, summary.SuccessRate*100)
			fmt.Printf("   Failed:   %d\n", summary.FailedCount)
			fmt.Printf("   PRs:      %d\n", summary.PRsCreated)
			fmt.Println()

			// Duration stats
			fmt.Println("⏱️  Duration")
			fmt.Printf("   Total:    %s\n", formatDuration(summary.TotalDurationMs))
			fmt.Printf("   Average:  %s\n", formatDuration(summary.AvgDurationMs))
			if summary.MinDurationMs > 0 {
				fmt.Printf("   Fastest:  %s\n", formatDuration(summary.MinDurationMs))
			}
			if summary.MaxDurationMs > 0 {
				fmt.Printf("   Slowest:  %s\n", formatDuration(summary.MaxDurationMs))
			}
			fmt.Println()

			// Token stats
			fmt.Println("🔤 Tokens")
			fmt.Printf("   Total:    %s\n", formatTokens(summary.TotalTokens))
			fmt.Printf("   Input:    %s\n", formatTokens(summary.TotalTokensInput))
			fmt.Printf("   Output:   %s\n", formatTokens(summary.TotalTokensOutput))
			if summary.TotalExecutions > 0 {
				fmt.Printf("   Avg/Task: %s\n", formatTokens(summary.AvgTokensPerTask))
			}
			fmt.Println()

			// Cost stats
			fmt.Println("💰 Estimated Cost")
			fmt.Printf("   Total:    $%.2f\n", summary.TotalCostUSD)
			if summary.TotalExecutions > 0 {
				fmt.Printf("   Avg/Task: $%.4f\n", summary.AvgCostUSD)
			}
			fmt.Println()

			// Code changes
			fmt.Println("📝 Code Changes")
			fmt.Printf("   Files:    %d\n", summary.TotalFilesChanged)
			fmt.Printf("   Added:    +%d lines\n", summary.TotalLinesAdded)
			fmt.Printf("   Removed:  -%d lines\n", summary.TotalLinesRemoved)
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().IntVar(&days, "days", 7, "Number of days to include")
	cmd.Flags().StringSliceVar(&projects, "projects", nil, "Filter by project paths")

	return cmd
}

func newMetricsDailyCmd() *cobra.Command {
	var (
		days     int
		projects []string
	)

	cmd := &cobra.Command{
		Use:   "daily",
		Short: "Show daily metrics breakdown",
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

			// Get daily metrics
			metrics, err := store.GetDailyMetrics(query)
			if err != nil {
				return fmt.Errorf("failed to get daily metrics: %w", err)
			}

			if len(metrics) == 0 {
				fmt.Println("No executions found in the specified period.")
				return nil
			}

			// Display daily breakdown
			fmt.Println()
			fmt.Printf("📅 Daily Metrics (Last %d Days)\n", days)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("%-12s %6s %6s %6s %10s %12s %10s\n", "Date", "Total", "Pass", "Fail", "Duration", "Tokens", "Cost")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			for _, m := range metrics {
				successRate := ""
				if m.ExecutionCount > 0 {
					rate := float64(m.SuccessCount) / float64(m.ExecutionCount) * 100
					successRate = fmt.Sprintf("%.0f%%", rate)
				}
				_ = successRate // unused for now, keeping format simple

				fmt.Printf("%-12s %6d %6d %6d %10s %12s %10s\n",
					m.Date.Format("2006-01-02"),
					m.ExecutionCount,
					m.SuccessCount,
					m.FailedCount,
					formatDurationShort(m.TotalDurationMs),
					formatTokensShort(m.TotalTokens),
					fmt.Sprintf("$%.2f", m.TotalCostUSD),
				)
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().IntVar(&days, "days", 7, "Number of days to include")
	cmd.Flags().StringSliceVar(&projects, "projects", nil, "Filter by project paths")

	return cmd
}
