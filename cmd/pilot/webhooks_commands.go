package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/webhooks"
)

func newWebhooksAddCmd() *cobra.Command {
	var (
		name    string
		url     string
		secret  string
		events  []string
		enabled bool
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new webhook endpoint",
		Long: `Add a new webhook endpoint to receive Pilot events.

Events:
  task.started, task.progress, task.completed, task.failed, pr.created, budget.warning

Examples:
  # Subscribe to all events
  pilot webhooks add --url https://example.com/hook --secret $SECRET

  # Subscribe to specific events
  pilot webhooks add --url https://example.com/hook --secret $SECRET \
    --events task.completed,task.failed,pr.created

  # With custom name
  pilot webhooks add --name "Slack Integration" --url https://hooks.slack.com/... --secret $SECRET`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				return fmt.Errorf("--url is required")
			}

			cfg, err := loadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			if cfg.Webhooks == nil {
				cfg.Webhooks = webhooks.DefaultConfig()
			}

			// Parse events
			var eventTypes []webhooks.EventType
			for _, e := range events {
				eventTypes = append(eventTypes, webhooks.EventType(e))
			}

			// Generate ID
			id := "ep_" + randomID(8)

			// Default name from URL if not provided
			if name == "" {
				name = extractHostFromURL(url)
			}

			endpoint := &webhooks.EndpointConfig{
				ID:      id,
				Name:    name,
				URL:     url,
				Secret:  os.ExpandEnv(secret),
				Events:  eventTypes,
				Enabled: enabled,
			}

			cfg.Webhooks.Endpoints = append(cfg.Webhooks.Endpoints, endpoint)

			// Save config
			configPath := config.DefaultConfigPath()
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("✓ Added webhook endpoint: %s (%s)\n", endpoint.Name, endpoint.ID)
			if !cfg.Webhooks.Enabled {
				fmt.Println()
				fmt.Println("Note: Webhooks are disabled. Enable in config:")
				fmt.Println("  webhooks:")
				fmt.Println("    enabled: true")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Endpoint name (optional)")
	cmd.Flags().StringVar(&url, "url", "", "Webhook URL (required)")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC signing secret")
	cmd.Flags().StringSliceVar(&events, "events", nil, "Event types to subscribe (default: all)")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Enable endpoint")

	return cmd
}

func newWebhooksRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <endpoint-id>",
		Short: "Remove a webhook endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			endpointID := args[0]

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if cfg.Webhooks == nil {
				return fmt.Errorf("no webhooks configured")
			}

			// Find and remove endpoint
			found := false
			var newEndpoints []*webhooks.EndpointConfig
			for _, ep := range cfg.Webhooks.Endpoints {
				if ep.ID == endpointID {
					found = true
					continue
				}
				newEndpoints = append(newEndpoints, ep)
			}

			if !found {
				return fmt.Errorf("endpoint not found: %s", endpointID)
			}

			cfg.Webhooks.Endpoints = newEndpoints

			// Save config
			configPath := config.DefaultConfigPath()
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("✓ Removed webhook endpoint: %s\n", endpointID)
			return nil
		},
	}
}

func newWebhooksTestCmd() *cobra.Command {
	var eventType string

	cmd := &cobra.Command{
		Use:   "test [endpoint-id]",
		Short: "Send a test event to webhook endpoint(s)",
		Long: `Send a test event to verify webhook endpoint configuration.

If no endpoint ID is specified, sends to all enabled endpoints.

Examples:
  pilot webhooks test                    # Test all enabled endpoints
  pilot webhooks test ep_abc123          # Test specific endpoint
  pilot webhooks test --event task.failed # Test with specific event type`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if cfg.Webhooks == nil || len(cfg.Webhooks.Endpoints) == 0 {
				return fmt.Errorf("no webhooks configured")
			}

			// Create manager
			manager := webhooks.NewManager(cfg.Webhooks, nil)

			// Create test event
			testEvent := webhooks.NewEvent(
				webhooks.EventType(eventType),
				map[string]interface{}{
					"task_id": "test_" + randomID(8),
					"title":   "Test Event",
					"project": "pilot",
					"message": "This is a test event from 'pilot webhooks test'",
				},
			)

			// Verify endpoint exists if ID specified
			if len(args) > 0 {
				ep := manager.GetEndpoint(args[0])
				if ep == nil {
					return fmt.Errorf("endpoint not found: %s", args[0])
				}
			}

			fmt.Println()
			fmt.Printf("Sending test event: %s\n", eventType)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Dispatch event
			results := manager.Dispatch(ctx, testEvent)

			// Show results
			for _, result := range results {
				ep := manager.GetEndpoint(result.EndpointID)
				name := result.EndpointID
				if ep != nil {
					name = ep.Name
				}

				if result.Success {
					fmt.Printf("  ✓ %s (status %d, %v)\n", name, result.StatusCode, result.Duration.Round(time.Millisecond))
				} else {
					fmt.Printf("  ✗ %s: %v\n", name, result.Error)
				}
			}

			// Summary
			successCount := 0
			for _, r := range results {
				if r.Success {
					successCount++
				}
			}

			fmt.Println()
			if len(results) == 0 {
				fmt.Println("No endpoints to test (all disabled or filtered)")
			} else {
				fmt.Printf("Results: %d/%d successful\n", successCount, len(results))
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVar(&eventType, "event", "task.completed", "Event type to test")

	return cmd
}

func newWebhooksEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List available webhook event types",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			fmt.Println("Available Webhook Events")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()
			fmt.Println("  Task Events:")
			fmt.Println("    task.started    - Task execution began")
			fmt.Println("    task.progress   - Phase updates during execution")
			fmt.Println("    task.completed  - Task finished successfully")
			fmt.Println("    task.failed     - Task failed")
			fmt.Println()
			fmt.Println("  PR Events:")
			fmt.Println("    pr.created      - Pull request was created")
			fmt.Println()
			fmt.Println("  Budget Events:")
			fmt.Println("    budget.warning  - Budget threshold reached")
			fmt.Println()
		},
	}
}
