package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/memory"
)

func newUsageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "View usage metering and billing data",
		Long:  `View billable usage events, summaries, and export data for billing.`,
	}

	cmd.AddCommand(
		newUsageSummaryCmd(),
		newUsageDailyCmd(),
		newUsageProjectsCmd(),
		newUsageEventsCmd(),
		newUsageExportCmd(),
	)

	return cmd
}

func newUsageSummaryCmd() *cobra.Command {
	var (
		days      int
		userID    string
		projectID string
	)

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show usage summary for billing",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			// Build query
			end := time.Now()
			start := end.AddDate(0, 0, -days)
			query := memory.UsageQuery{
				UserID:    userID,
				ProjectID: projectID,
				Start:     start,
				End:       end,
			}

			// Get summary
			summary, err := store.GetUsageSummary(query)
			if err != nil {
				return fmt.Errorf("failed to get usage summary: %w", err)
			}

			// Display summary
			fmt.Println()
			fmt.Printf("💰 Usage Summary (Last %d Days)\n", days)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			// Task usage
			fmt.Println("📋 Tasks")
			fmt.Printf("   Count:    %d\n", summary.TaskCount)
			fmt.Printf("   Cost:     $%.2f\n", summary.TaskCost)
			fmt.Println()

			// Token usage
			fmt.Println("🔤 Tokens")
			fmt.Printf("   Input:    %s\n", formatTokens(summary.TokensInput))
			fmt.Printf("   Output:   %s\n", formatTokens(summary.TokensOutput))
			fmt.Printf("   Total:    %s\n", formatTokens(summary.TokensTotal))
			fmt.Printf("   Cost:     $%.2f\n", summary.TokenCost)
			fmt.Println()

			// Compute usage
			fmt.Println("⚡ Compute")
			fmt.Printf("   Minutes:  %d\n", summary.ComputeMinutes)
			fmt.Printf("   Cost:     $%.2f\n", summary.ComputeCost)
			fmt.Println()

			// Storage (if any)
			if summary.StorageBytes > 0 {
				fmt.Println("💾 Storage")
				fmt.Printf("   Bytes:    %s\n", formatBytes(summary.StorageBytes))
				fmt.Printf("   Cost:     $%.2f\n", summary.StorageCost)
				fmt.Println()
			}

			// API calls (if any)
			if summary.APICallCount > 0 {
				fmt.Println("🌐 API Calls")
				fmt.Printf("   Count:    %d\n", summary.APICallCount)
				fmt.Printf("   Cost:     $%.2f\n", summary.APICallCost)
				fmt.Println()
			}

			// Total
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("💵 TOTAL COST:  $%.2f\n", summary.TotalCost)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "Number of days to include")
	cmd.Flags().StringVar(&userID, "user", "", "Filter by user ID")
	cmd.Flags().StringVar(&projectID, "project", "", "Filter by project ID")

	return cmd
}

func newUsageDailyCmd() *cobra.Command {
	var (
		days      int
		userID    string
		projectID string
	)

	cmd := &cobra.Command{
		Use:   "daily",
		Short: "Show daily usage breakdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			// Build query
			end := time.Now()
			start := end.AddDate(0, 0, -days)
			query := memory.UsageQuery{
				UserID:    userID,
				ProjectID: projectID,
				Start:     start,
				End:       end,
			}

			// Get daily usage
			usage, err := store.GetDailyUsage(query)
			if err != nil {
				return fmt.Errorf("failed to get daily usage: %w", err)
			}

			if len(usage) == 0 {
				fmt.Println("No usage data found in the specified period.")
				return nil
			}

			// Display daily breakdown
			fmt.Println()
			fmt.Printf("📅 Daily Usage (Last %d Days)\n", days)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("%-12s %8s %10s %12s %10s %10s\n", "Date", "Tasks", "Task $", "Tokens", "Token $", "Total $")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			for _, u := range usage {
				fmt.Printf("%-12s %8d %10s %12s %10s %10s\n",
					u.Date.Format("2006-01-02"),
					u.TaskCount,
					fmt.Sprintf("$%.2f", u.TaskCost),
					formatTokensShort(u.TokenCount),
					fmt.Sprintf("$%.2f", u.TokenCost),
					fmt.Sprintf("$%.2f", u.TotalCost),
				)
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().IntVar(&days, "days", 7, "Number of days to include")
	cmd.Flags().StringVar(&userID, "user", "", "Filter by user ID")
	cmd.Flags().StringVar(&projectID, "project", "", "Filter by project ID")

	return cmd
}
