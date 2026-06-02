package main

import (
	"context"
	"fmt"

	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/memory"
)

// checkTaskBudget runs the pre-execution budget gate for the task command.
//
// The memory store it opens must be closed at the *outer* RunE return, so this
// helper returns a closer that the caller registers with defer the moment it is
// non-nil. closer is non-nil once the store has been opened (mirroring the
// original `defer store.Close()` placement), and any subsequent setup error,
// budget-check error, or budget-block error is returned via err so the caller
// reproduces the exact same `return` while the deferred Close still fires.
func checkTaskBudget(ctx context.Context) (closer func() error, err error) {
	// Load config for budget
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	budgetCfg, loadErr := config.Load(configPath)
	if loadErr != nil {
		return nil, fmt.Errorf("failed to load config for budget: %w", loadErr)
	}

	// Get budget config or use defaults
	budgetConfig := budgetCfg.Budget
	if budgetConfig == nil {
		budgetConfig = budget.DefaultConfig()
	}

	// Enable budget check even if not enabled in config (flag overrides)
	budgetConfig.Enabled = true

	// Open memory store for usage data
	store, storeErr := memory.NewStore(budgetCfg.Memory.Path)
	if storeErr != nil {
		return nil, fmt.Errorf("failed to open memory store for budget: %w", storeErr)
	}
	closer = func() error { return store.Close() }

	// Create budget enforcer and check
	enforcer := budget.NewEnforcer(budgetConfig, store)
	result, checkErr := enforcer.CheckBudget(ctx, "", "")
	if checkErr != nil {
		return closer, fmt.Errorf("budget check failed: %w", checkErr)
	}

	if !result.Allowed {
		fmt.Println()
		fmt.Println("🚫 Task Blocked by Budget")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("   Reason: %s\n", result.Reason)
		fmt.Println()
		fmt.Println("   Run 'pilot budget status' for details")
		fmt.Println("   Run 'pilot budget reset' to reset daily counters")
		fmt.Println()
		return closer, fmt.Errorf("task blocked by budget: %s", result.Reason)
	}

	// Show budget status
	fmt.Printf("   Budget:    ✓ $%.2f daily / $%.2f monthly remaining\n", result.DailyLeft, result.MonthlyLeft)
	return closer, nil
}
