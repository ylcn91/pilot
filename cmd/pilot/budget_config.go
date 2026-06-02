package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

func newBudgetConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show budget configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			budgetCfg := cfg.Budget
			if budgetCfg == nil {
				budgetCfg = budget.DefaultConfig()
			}

			fmt.Println()
			fmt.Println(budgetHeaderStyle.Render("BUDGET CONFIGURATION"))
			fmt.Println(budgetDivider())
			fmt.Println()
			fmt.Printf("  Enabled:       %v\n", budgetCfg.Enabled)
			fmt.Printf("  Daily Limit:   $%.2f\n", budgetCfg.DailyLimit)
			fmt.Printf("  Monthly Limit: $%.2f\n", budgetCfg.MonthlyLimit)
			fmt.Println()
			fmt.Println("  Per-Task Limits:")
			fmt.Printf("    Max Tokens:   %d\n", budgetCfg.PerTask.MaxTokens)
			fmt.Printf("    Max Duration: %s\n", budgetCfg.PerTask.MaxDuration)
			fmt.Println()
			fmt.Println("  On Exceed Actions:")
			fmt.Printf("    Daily:   %s\n", budgetCfg.OnExceed.Daily)
			fmt.Printf("    Monthly: %s\n", budgetCfg.OnExceed.Monthly)
			fmt.Printf("    PerTask: %s\n", budgetCfg.OnExceed.PerTask)
			fmt.Println()
			fmt.Println("  Thresholds:")
			fmt.Printf("    Warning at: %.0f%%\n", budgetCfg.Thresholds.WarnPercent)
			fmt.Println()
			fmt.Println(budgetDivider())
			fmt.Println()
			fmt.Println("  YAML configuration example:")
			fmt.Println(budgetDimStyle.Render("  " + strings.Repeat("─", 37)))
			fmt.Println("  budget:")
			fmt.Println("    enabled: true")
			fmt.Println("    daily_limit: 50.00")
			fmt.Println("    monthly_limit: 500.00")
			fmt.Println("    per_task:")
			fmt.Println("      max_tokens: 100000")
			fmt.Println("      max_duration: 30m")
			fmt.Println("    on_exceed:")
			fmt.Println("      daily: pause")
			fmt.Println("      monthly: stop")
			fmt.Println("      per_task: stop")
			fmt.Println("    thresholds:")
			fmt.Println("      warn_percent: 80")
			fmt.Println(budgetDimStyle.Render("  " + strings.Repeat("─", 37)))
			fmt.Println()

			return nil
		},
	}

	return cmd
}

func newBudgetResetCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset blocked tasks counter",
		Long:  `Reset the blocked tasks counter and resume task execution if paused due to daily limits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				fmt.Println()
				fmt.Println(budgetWarnStyle.Render("[!] This will reset the blocked tasks counter and may resume paused execution."))
				fmt.Println("    Use --confirm to proceed.")
				fmt.Println()
				return nil
			}

			// Load config
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Open store
			store, err := memory.NewStore(cfg.Memory.Path)
			if err != nil {
				return fmt.Errorf("failed to open memory store: %w", err)
			}
			defer func() { _ = store.Close() }()

			// Create enforcer and reset
			budgetCfg := cfg.Budget
			if budgetCfg == nil {
				budgetCfg = budget.DefaultConfig()
			}

			enforcer := budget.NewEnforcer(budgetCfg, store)
			enforcer.ResetDaily()

			fmt.Println()
			fmt.Println("[OK] Budget counters reset")
			fmt.Println("     Blocked tasks counter: 0")
			fmt.Println("     Daily pause status: cleared")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm the reset operation")

	return cmd
}

func newBudgetSetCmd() *cobra.Command {
	var (
		daily   float64
		monthly float64
		enabled bool
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set budget limits",
		Long: `Update budget limits in the configuration.

Examples:
  pilot budget set --daily 50       # Set daily limit to $50
  pilot budget set --monthly 500    # Set monthly limit to $500
  pilot budget set --enabled        # Enable budget enforcement
  pilot budget set --daily 100 --monthly 1000 --enabled`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Initialize budget config if nil
			if cfg.Budget == nil {
				cfg.Budget = budget.DefaultConfig()
			}

			// Track what changed
			changes := []string{}

			// Apply changes
			if cmd.Flags().Changed("daily") {
				cfg.Budget.DailyLimit = daily
				changes = append(changes, fmt.Sprintf("daily_limit: $%.2f", daily))
			}
			if cmd.Flags().Changed("monthly") {
				cfg.Budget.MonthlyLimit = monthly
				changes = append(changes, fmt.Sprintf("monthly_limit: $%.2f", monthly))
			}
			if cmd.Flags().Changed("enabled") {
				cfg.Budget.Enabled = enabled
				changes = append(changes, fmt.Sprintf("enabled: %v", enabled))
			}

			if len(changes) == 0 {
				fmt.Println("No changes specified. Use flags to set values:")
				fmt.Println("  --daily <amount>     Set daily limit (USD)")
				fmt.Println("  --monthly <amount>   Set monthly limit (USD)")
				fmt.Println("  --enabled            Enable budget enforcement")
				return nil
			}

			// Save updated config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Println()
			fmt.Println(budgetHeaderStyle.Render("BUDGET UPDATED"))
			fmt.Println(budgetDivider())
			for _, change := range changes {
				fmt.Printf("  ✓ %s\n", change)
			}
			fmt.Println()
			fmt.Printf("  Config saved to: %s\n", configPath)
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().Float64Var(&daily, "daily", 0, "Daily budget limit (USD)")
	cmd.Flags().Float64Var(&monthly, "monthly", 0, "Monthly budget limit (USD)")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Enable budget enforcement")

	return cmd
}

func newBudgetAlertCmd() *cobra.Command {
	var (
		warnPercent float64
		onDaily     string
		onMonthly   string
	)

	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Configure budget alerts",
		Long: `Configure alert thresholds and actions when budget limits are approached or exceeded.

Actions:
  warn   - Log warning but continue processing
  pause  - Stop accepting new tasks, finish current
  stop   - Terminate immediately

Examples:
  pilot budget alert --warn-at 80                    # Warn at 80% usage
  pilot budget alert --on-daily pause               # Pause on daily limit
  pilot budget alert --on-monthly stop              # Stop on monthly limit
  pilot budget alert --warn-at 90 --on-daily warn   # Late warning, warn only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Initialize budget config if nil
			if cfg.Budget == nil {
				cfg.Budget = budget.DefaultConfig()
			}

			// Track what changed
			changes := []string{}

			// Apply changes
			if cmd.Flags().Changed("warn-at") {
				cfg.Budget.Thresholds.WarnPercent = warnPercent
				changes = append(changes, fmt.Sprintf("warn_percent: %.0f%%", warnPercent))
			}
			if cmd.Flags().Changed("on-daily") {
				action := budget.Action(onDaily)
				if action != budget.ActionWarn && action != budget.ActionPause && action != budget.ActionStop {
					return fmt.Errorf("invalid action: %s (use warn, pause, or stop)", onDaily)
				}
				cfg.Budget.OnExceed.Daily = action
				changes = append(changes, fmt.Sprintf("on_exceed.daily: %s", onDaily))
			}
			if cmd.Flags().Changed("on-monthly") {
				action := budget.Action(onMonthly)
				if action != budget.ActionWarn && action != budget.ActionPause && action != budget.ActionStop {
					return fmt.Errorf("invalid action: %s (use warn, pause, or stop)", onMonthly)
				}
				cfg.Budget.OnExceed.Monthly = action
				changes = append(changes, fmt.Sprintf("on_exceed.monthly: %s", onMonthly))
			}

			if len(changes) == 0 {
				// Show current alert config
				fmt.Println()
				fmt.Println(budgetHeaderStyle.Render("BUDGET ALERT CONFIGURATION"))
				fmt.Println(budgetDivider())
				fmt.Println()
				fmt.Printf("  Warning threshold:  %.0f%%\n", cfg.Budget.Thresholds.WarnPercent)
				fmt.Println()
				fmt.Println("  On limit exceeded:")
				fmt.Printf("    Daily:   %s\n", cfg.Budget.OnExceed.Daily)
				fmt.Printf("    Monthly: %s\n", cfg.Budget.OnExceed.Monthly)
				fmt.Printf("    PerTask: %s\n", cfg.Budget.OnExceed.PerTask)
				fmt.Println()
				fmt.Println(budgetDivider())
				fmt.Println()
				fmt.Println("  Use flags to update:")
				fmt.Println("    --warn-at <percent>   Warning threshold percentage")
				fmt.Println("    --on-daily <action>   Action when daily limit exceeded")
				fmt.Println("    --on-monthly <action> Action when monthly limit exceeded")
				fmt.Println()
				return nil
			}

			// Save updated config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Println()
			fmt.Println(budgetHeaderStyle.Render("ALERT CONFIG UPDATED"))
			fmt.Println(budgetDivider())
			for _, change := range changes {
				fmt.Printf("  ✓ %s\n", change)
			}
			fmt.Println()
			fmt.Printf("  Config saved to: %s\n", configPath)
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().Float64Var(&warnPercent, "warn-at", 0, "Warning threshold percentage (e.g., 80)")
	cmd.Flags().StringVar(&onDaily, "on-daily", "", "Action when daily limit exceeded (warn, pause, stop)")
	cmd.Flags().StringVar(&onMonthly, "on-monthly", "", "Action when monthly limit exceeded (warn, pause, stop)")

	return cmd
}
