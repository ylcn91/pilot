package main

import "github.com/spf13/cobra"

// startFlags holds the values bound to the `pilot start` command flags. The
// fields are populated by cobra during flag parsing and consumed inside the
// RunE closure.
type startFlags struct {
	dashboardMode bool
	projectPath   string
	replace       bool
	// Input adapter flags (override config) - use bool with "changed" check
	enableTelegram bool
	enableGithub   bool
	enableLinear   bool
	enableSlack    bool
	enablePlane    bool
	enableDiscord  bool
	// Mode flags
	noGateway    bool   // Lightweight mode: polling only, no HTTP gateway
	sequential   bool   // Sequential execution mode (one issue at a time)
	envFlag      string // Environment name: dev, stage, prod, or custom configured name
	enableTunnel bool   // Enable public tunnel (Cloudflare/ngrok)
	teamID       string // Optional team ID for scoping execution
	teamMember   string // Member email for project access scoping
	logFormat    string // Log output format: text or json (GH-847)
}

// registerStartFlags registers the `pilot start` flags onto cmd, binding each
// to the corresponding field of f. Flag names, defaults, usage strings, and
// registration order are preserved exactly so command behavior is unchanged.
func registerStartFlags(cmd *cobra.Command, f *startFlags) {
	cmd.Flags().BoolVar(&f.dashboardMode, "dashboard", false, "Show TUI dashboard for real-time task monitoring")
	cmd.Flags().StringVarP(&f.projectPath, "project", "p", "", "Project path (default: config default or cwd)")
	cmd.Flags().BoolVar(&f.replace, "replace", false, "Kill existing bot instance before starting")
	cmd.Flags().BoolVar(&f.noGateway, "no-gateway", false, "Run polling adapters only (no HTTP gateway)")
	cmd.Flags().BoolVar(&f.sequential, "sequential", false, "Sequential execution: wait for PR merge before next issue")
	cmd.Flags().StringVar(&f.envFlag, "env", "",
		"Environment name: dev, stage, prod, or custom configured environment")
	// Keep --autopilot as hidden deprecated alias
	cmd.Flags().StringVar(&f.envFlag, "autopilot", "",
		"DEPRECATED: Use --env instead")
	_ = cmd.Flags().MarkHidden("autopilot")

	// Input adapter flags - standard bool flags
	cmd.Flags().BoolVar(&f.enableTelegram, "telegram", false, "Enable Telegram polling (overrides config)")
	cmd.Flags().BoolVar(&f.enableGithub, "github", false, "Enable GitHub polling (overrides config)")
	cmd.Flags().BoolVar(&f.enableLinear, "linear", false, "Enable Linear webhooks (overrides config)")
	cmd.Flags().BoolVar(&f.enableSlack, "slack", false, "Enable Slack Socket Mode (overrides config)")
	cmd.Flags().BoolVar(&f.enablePlane, "plane", false, "Enable Plane.so polling (overrides config)")
	cmd.Flags().BoolVar(&f.enableDiscord, "discord", false, "Enable Discord bot (overrides config)")
	cmd.Flags().BoolVar(&f.enableTunnel, "tunnel", false, "Enable public tunnel for webhook ingress (Cloudflare/ngrok)")
	cmd.Flags().StringVar(&f.teamID, "team", "", "Team ID or name for project access scoping (overrides config)")
	cmd.Flags().StringVar(&f.teamMember, "team-member", "", "Member email for team access scoping (overrides config)")
	cmd.Flags().StringVar(&f.logFormat, "log-format", "text", "Log output format: text or json (for log aggregation systems)")
}
