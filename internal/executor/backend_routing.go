package executor

// ModelRoutingConfig controls which model to use based on task complexity.
// Enables cost optimization by using cheaper models for simple tasks.
//
// Example YAML configuration:
//
//	executor:
//	  model_routing:
//	    enabled: true
//	    trivial: "claude-haiku"    # Typos, log additions, renames
//	    simple: "claude-sonnet"    # Small fixes, add fields
//	    medium: "claude-sonnet"    # Standard feature work
//	    complex: "claude-opus"     # Refactors, migrations
//
// Task complexity is auto-detected from the issue description and labels.
// When enabled, reduces costs by ~40% while maintaining quality for complex tasks.
type ModelRoutingConfig struct {
	// Enabled controls whether model routing is active.
	// When false (default), the orchestrator's model setting is used for all tasks.
	Enabled bool `yaml:"enabled"`

	// Trivial is the model for trivial tasks (typos, log additions, renames).
	// Default: "claude-haiku"
	Trivial string `yaml:"trivial"`

	// Simple is the model for simple tasks (small fixes, add field, update config).
	// Default: "claude-sonnet"
	Simple string `yaml:"simple"`

	// Medium is the model for standard feature work (new endpoints, components).
	// Default: "claude-sonnet"
	Medium string `yaml:"medium"`

	// Complex is the model for architectural work (refactors, migrations, new systems).
	// Default: "claude-opus"
	Complex string `yaml:"complex"`
}

// TimeoutConfig controls execution timeouts to prevent stuck tasks.
type TimeoutConfig struct {
	// Default is the default timeout for all tasks
	Default string `yaml:"default"`

	// Trivial is the timeout for trivial tasks (shorter)
	Trivial string `yaml:"trivial"`

	// Simple is the timeout for simple tasks
	Simple string `yaml:"simple"`

	// Medium is the timeout for medium tasks
	Medium string `yaml:"medium"`

	// Complex is the timeout for complex tasks (longer)
	Complex string `yaml:"complex"`
}

// EffortRoutingConfig controls the effort level based on task complexity.
// Effort controls how many tokens Claude uses when responding — trading off
// between thoroughness and efficiency. Works with Claude API output_config.effort.
//
// Example YAML configuration:
//
//	executor:
//	  effort_routing:
//	    enabled: true
//	    trivial: "low"     # Fast, minimal token spend
//	    simple: "medium"   # Balanced
//	    medium: "high"     # Standard (default behavior)
//	    complex: "max"     # Deepest reasoning
type EffortRoutingConfig struct {
	// Enabled controls whether effort routing is active.
	// When false (default), effort is not set (uses model default of "high").
	Enabled bool `yaml:"enabled"`

	// Trivial effort for trivial tasks. Default: "low"
	Trivial string `yaml:"trivial"`

	// Simple effort for simple tasks. Default: "medium"
	Simple string `yaml:"simple"`

	// Medium effort for standard tasks. Default: "high"
	Medium string `yaml:"medium"`

	// Complex effort for architectural work. Default: "max"
	Complex string `yaml:"complex"`
}

// EffortClassifierConfig configures the LLM-based effort classifier that analyzes
// task content to recommend the appropriate effort level before execution.
// Falls back to static complexity→effort mapping on failure.
//
// GH-727: Smarter effort selection via LLM analysis.
// Cost: ~$0.0002 per classification (negligible vs execution savings).
//
// Example YAML configuration:
//
//	executor:
//	  effort_classifier:
//	    enabled: true
//	    model: "claude-haiku-4-5-20251001"
//	    timeout: 30s
type EffortClassifierConfig struct {
	// Enabled controls whether LLM effort classification is active.
	// When false (default), static complexity→effort mapping is used.
	Enabled bool `yaml:"enabled"`

	// Model is the model to use for effort classification.
	// Default: "claude-haiku-4-5-20251001"
	Model string `yaml:"model,omitempty"`

	// Timeout is the maximum time to wait for LLM response.
	// Default: "30s"
	Timeout string `yaml:"timeout,omitempty"`
}

// DefaultEffortClassifierConfig returns default effort classifier configuration.
func DefaultEffortClassifierConfig() *EffortClassifierConfig {
	return &EffortClassifierConfig{
		Enabled: true,
		Model:   "claude-haiku-4-5-20251001",
		Timeout: "30s",
	}
}

// DefaultModelRoutingConfig returns default model routing configuration.
// Model routing is disabled by default; when enabled, uses Haiku for trivial
// tasks (speed), Sonnet 4.6 for simple/medium tasks (near-Opus quality at 40%
// lower cost), and Opus 4.6 for complex tasks (highest capability).
//
// Complexity detection criteria:
//   - Trivial: Single-file changes, typos, logging, renames
//   - Simple: Small fixes, add/remove fields, config updates
//   - Medium: New endpoints, components, moderate refactoring
//   - Complex: Architecture changes, multi-file refactors, migrations
func DefaultModelRoutingConfig() *ModelRoutingConfig {
	return &ModelRoutingConfig{
		Enabled: true,
		Trivial: "claude-haiku",
		Simple:  "claude-sonnet-4-6",
		Medium:  "claude-sonnet-4-6",
		// GH-2432: Sonnet for "complex" too — Opus is reserved for planning only.
		Complex: "claude-sonnet-4-6",
	}
}

// DefaultEffortRoutingConfig returns default effort routing configuration.
// Effort routing is disabled by default; when enabled, maps task complexity
// to Claude API effort levels for optimal cost/quality trade-off.
func DefaultEffortRoutingConfig() *EffortRoutingConfig {
	return &EffortRoutingConfig{
		Enabled: false,
		Trivial: "low",
		Simple:  "medium",
		Medium:  "high",
		Complex: "max",
	}
}

// DefaultTimeoutConfig returns default timeout configuration.
// Timeouts are calibrated to prevent stuck tasks while allowing complex work.
func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		Default: "30m",
		Trivial: "5m",
		Simple:  "10m",
		Medium:  "30m",
		Complex: "60m",
	}
}
