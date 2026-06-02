package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/webhooks"
)

func newWebhooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhooks",
		Short: "Manage outbound webhooks",
		Long: `Manage outbound webhooks for Pilot events.

Webhooks allow external integrations to receive real-time notifications
when tasks start, complete, fail, or when PRs are created.

Supported events:
  - task.started    Task execution began
  - task.progress   Phase updates during execution
  - task.completed  Task finished successfully
  - task.failed     Task failed
  - pr.created      Pull request was created
  - budget.warning  Budget threshold reached

Examples:
  pilot webhooks list                              # List configured webhooks
  pilot webhooks add --url https://example.com/hook --secret $SECRET
  pilot webhooks remove ep_abc123
  pilot webhooks test ep_abc123                    # Send test event`,
	}

	cmd.AddCommand(
		newWebhooksListCmd(),
		newWebhooksAddCmd(),
		newWebhooksRemoveCmd(),
		newWebhooksTestCmd(),
		newWebhooksEventsCmd(),
	)

	return cmd
}

func newWebhooksListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured webhook endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			webhooksCfg := cfg.Webhooks
			if webhooksCfg == nil {
				webhooksCfg = webhooks.DefaultConfig()
			}

			if outputJSON {
				data, err := json.MarshalIndent(webhooksCfg.Endpoints, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal webhooks: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			fmt.Println()
			fmt.Println("Webhook Endpoints")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			if !webhooksCfg.Enabled {
				fmt.Println("  Status: ○ Disabled")
				fmt.Println()
				fmt.Println("  Enable webhooks in ~/.pilot/config.yaml:")
				fmt.Println("    webhooks:")
				fmt.Println("      enabled: true")
				fmt.Println()
				return nil
			}

			if len(webhooksCfg.Endpoints) == 0 {
				fmt.Println("  No endpoints configured")
				fmt.Println()
				fmt.Println("  Add an endpoint:")
				fmt.Println("    pilot webhooks add --url https://example.com/hook --secret $SECRET")
				fmt.Println()
				return nil
			}

			for _, ep := range webhooksCfg.Endpoints {
				statusIcon := "○"
				if ep.Enabled {
					statusIcon = "✓"
				}

				fmt.Printf("\n  %s %s\n", statusIcon, ep.Name)
				fmt.Printf("    ID:     %s\n", ep.ID)
				fmt.Printf("    URL:    %s\n", ep.URL)
				if len(ep.Events) > 0 {
					events := make([]string, len(ep.Events))
					for i, e := range ep.Events {
						events[i] = string(e)
					}
					fmt.Printf("    Events: %s\n", strings.Join(events, ", "))
				} else {
					fmt.Printf("    Events: all\n")
				}
				if ep.Secret != "" {
					fmt.Printf("    Secret: ****%s\n", lastN(ep.Secret, 4))
				}
			}

			fmt.Println()
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

// Helper functions

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func extractHostFromURL(urlStr string) string {
	// Simple extraction - just get host part
	urlStr = strings.TrimPrefix(urlStr, "https://")
	urlStr = strings.TrimPrefix(urlStr, "http://")
	parts := strings.Split(urlStr, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return urlStr
}

func randomID(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}
