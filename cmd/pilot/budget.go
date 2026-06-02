package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/ylcn91/pilot/internal/config"
)

// Budget status TUI constants
const (
	budgetWidth    = 60
	budgetBarWidth = 40
	budgetLabelCol = 14
)

// Budget status TUI styles
var (
	budgetHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("255"))

	budgetDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	budgetWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	budgetErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))

	budgetBarNormal = lipgloss.Color("255")
	budgetBarWarn   = lipgloss.Color("214")
	budgetBarError  = lipgloss.Color("196")
)

func budgetDivider() string {
	return budgetDimStyle.Render(strings.Repeat("─", budgetWidth))
}

func newBudgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "View and manage cost controls",
		Long:  `View budget status, limits, and manage cost controls.`,
	}

	cmd.AddCommand(
		newBudgetStatusCmd(),
		newBudgetConfigCmd(),
		newBudgetSetCmd(),
		newBudgetResetCmd(),
		newBudgetAlertCmd(),
	)

	return cmd
}

// loadConfig loads the application configuration
func loadConfig() (*config.Config, error) {
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, nil
}

// Note: formatTokens is defined in metrics.go and shared across CLI commands
