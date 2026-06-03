package config

import (
	"fmt"
	"log"
	"strings"

	"github.com/ylcn91/pilot/internal/gateway"
)

// validEffortLevels are the effort levels supported by Claude Code CLI.
var validEffortLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"max":    true,
	"":       true, // Empty uses default
}

// Validate checks the configuration for errors and returns an error if invalid.
// It validates required fields, port ranges, authentication settings, and routing config.
func (c *Config) Validate() error {
	if c.Gateway == nil {
		return fmt.Errorf("gateway configuration is required")
	}
	if c.Gateway.Port < 1 || c.Gateway.Port > 65535 {
		return fmt.Errorf("invalid gateway port: %d", c.Gateway.Port)
	}
	if c.Auth != nil && c.Auth.Type == gateway.AuthTypeAPIToken && c.Auth.Token == "" {
		return fmt.Errorf("API token is required when auth type is api-token")
	}

	// GH-914: Validate effort routing if enabled
	if c.Executor != nil && c.Executor.EffortRouting != nil && c.Executor.EffortRouting.Enabled {
		levels := map[string]string{
			"trivial": c.Executor.EffortRouting.Trivial,
			"simple":  c.Executor.EffortRouting.Simple,
			"medium":  c.Executor.EffortRouting.Medium,
			"complex": c.Executor.EffortRouting.Complex,
		}
		for name, value := range levels {
			normalized := strings.ToLower(strings.TrimSpace(value))
			if !validEffortLevels[normalized] {
				return fmt.Errorf("invalid effort_routing.%s: %q (must be low, medium, high, or max)", name, value)
			}
		}
	}

	// Validate optional per-phase backend pipeline (fail fast on a bad stage).
	if c.Executor != nil && c.Executor.Pipeline != nil {
		if err := c.Executor.Pipeline.Validate(); err != nil {
			return fmt.Errorf("executor.%w", err)
		}
	}

	// Validate optional opt-in TDD mode (fail fast on a bad role backend).
	if c.Executor != nil && c.Executor.TDD != nil {
		if err := c.Executor.TDD.Validate(); err != nil {
			return fmt.Errorf("executor.%w", err)
		}
	}

	// Validate optional proactive Architect pipeline (fail fast on a bad
	// backend / cron / ticket cap). Inert when nil or disabled.
	if err := c.Architect.Validate(); err != nil {
		return err
	}

	// Validate default project exists if specified
	if c.DefaultProject != "" && len(c.Projects) > 0 {
		found := false
		for _, p := range c.Projects {
			if p.Name == c.DefaultProject {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("default_project %q not found in projects list", c.DefaultProject)
		}
	}

	// GH-1124: Validate bounds and orchestrator configuration
	if c.Orchestrator != nil {
		// Validate max_concurrent >= 1
		if c.Orchestrator.MaxConcurrent < 1 {
			return fmt.Errorf("orchestrator.max_concurrent must be >= 1, got %d", c.Orchestrator.MaxConcurrent)
		}

		// Validate execution mode
		if c.Orchestrator.Execution != nil {
			validModes := map[string]bool{"sequential": true, "parallel": true, "auto": true}
			if !validModes[c.Orchestrator.Execution.Mode] {
				return fmt.Errorf("orchestrator.execution.mode must be 'sequential', 'parallel', or 'auto', got %q", c.Orchestrator.Execution.Mode)
			}
		}
	}

	// Validate quality on_failure max_retries in [0, 10]
	if c.Quality != nil && (c.Quality.OnFailure.MaxRetries < 0 || c.Quality.OnFailure.MaxRetries > 10) {
		return fmt.Errorf("quality.on_failure.max_retries must be in range [0, 10], got %d", c.Quality.OnFailure.MaxRetries)
	}

	// Validate budget daily_limit > 0 when budget is enabled
	if c.Budget != nil && c.Budget.Enabled && c.Budget.DailyLimit <= 0 {
		return fmt.Errorf("budget.daily_limit must be > 0 when budget is enabled, got %g", c.Budget.DailyLimit)
	}

	return nil
}

// CheckDeprecations logs warnings for deprecated configuration fields.
// Call this after loading configuration to inform users of deprecated settings.
// Returns a slice of deprecation warnings for testing purposes.
func (c *Config) CheckDeprecations() []string {
	var warnings []string

	// Check DailyBrief.Time (deprecated in favor of Schedule)
	if c.Orchestrator != nil && c.Orchestrator.DailyBrief != nil {
		if c.Orchestrator.DailyBrief.Time != "" {
			msg := "config: orchestrator.daily_brief.time is deprecated, use schedule (cron syntax) instead"
			log.Printf("DEPRECATED: %s", msg)
			warnings = append(warnings, msg)
		}
	}

	return warnings
}
