// Dashboard progress test - GH-151
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version   = "1.0.0"
	buildTime = "unknown"
	cfgFile   string
)

var quietMode bool

func main() {
	rootCmd := &cobra.Command{
		Use:   "pilot",
		Short: "AI that ships your tickets",
		Long:  `Pilot is an autonomous AI development pipeline that receives tickets, implements features, and creates PRs.`,
		Run: func(cmd *cobra.Command, args []string) {
			// If no subcommand provided, enter interactive mode
			if err := runInteractiveMode(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.pilot/config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&quietMode, "quiet", "q", false, "Suppress non-essential output")

	rootCmd.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newInitCmd(),
		newVersionCmd(),
		newTaskCmd(),
		newGitHubCmd(),
		newBriefCmd(),
		newPatternsCmd(),
		newMetricsCmd(),
		newUsageCmd(),
		newTeamCmd(),
		newBudgetCmd(),
		newDoctorCmd(),
		newSetupCmd(),
		newReplayCmd(),
		newTunnelCmd(),
		newCompletionCmd(),
		newConfigCmd(),
		newLogsCmd(),
		newWebhooksCmd(),
		newUpgradeCmd(),
		newReleaseCmd(),
		newAllowCmd(),
		newProjectCmd(),
		newAutopilotCmd(),
		newOnboardCmd(),
		newBackendCmd(),
		newChatCmd(),
		newArchitectCmd(),
	)

	// Install the process-wide panic-recovery telemetry hook so recovered
	// goroutine panics increment pilot_panics_total{component} and feed the
	// gateway liveness tracker (GH-31).
	installPanicHook()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
