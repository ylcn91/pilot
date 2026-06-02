package approval

import (
	"testing"
	"time"
)

func TestManager_DisabledByDefault(t *testing.T) {
	m := NewManager(nil)
	if m.IsEnabled() {
		t.Error("expected manager to be disabled by default")
	}
}

func TestManager_IsStageEnabled(t *testing.T) {
	tests := []struct {
		name     string
		stage    Stage
		setup    func(*Config)
		expected bool
	}{
		{
			name:  "pre_execution disabled",
			stage: StagePreExecution,
			setup: func(c *Config) {
				c.Enabled = true
				c.PreExecution.Enabled = false
			},
			expected: false,
		},
		{
			name:  "pre_execution enabled",
			stage: StagePreExecution,
			setup: func(c *Config) {
				c.Enabled = true
				c.PreExecution.Enabled = true
			},
			expected: true,
		},
		{
			name:  "pre_merge enabled",
			stage: StagePreMerge,
			setup: func(c *Config) {
				c.Enabled = true
				c.PreMerge.Enabled = true
			},
			expected: true,
		},
		{
			name:  "post_failure enabled",
			stage: StagePostFailure,
			setup: func(c *Config) {
				c.Enabled = true
				c.PostFailure.Enabled = true
			},
			expected: true,
		},
		{
			name:  "global disabled overrides stage",
			stage: StagePreExecution,
			setup: func(c *Config) {
				c.Enabled = false
				c.PreExecution.Enabled = true
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			tt.setup(config)
			m := NewManager(config)

			if got := m.IsStageEnabled(tt.stage); got != tt.expected {
				t.Errorf("IsStageEnabled(%s) = %v, want %v", tt.stage, got, tt.expected)
			}
		})
	}
}

func TestManager_IsStageEnabled_UnknownStage(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true

	m := NewManager(config)

	// Unknown stage should return false
	if m.IsStageEnabled(Stage("unknown_stage")) {
		t.Error("expected unknown stage to return false")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Verify basic structure
	if config == nil {
		t.Fatal("expected non-nil config")
	}

	if config.Enabled {
		t.Error("expected disabled by default")
	}

	if config.DefaultTimeout != 1*time.Hour {
		t.Errorf("expected default timeout 1 hour, got %v", config.DefaultTimeout)
	}

	if config.DefaultAction != DecisionRejected {
		t.Errorf("expected default action rejected, got %s", config.DefaultAction)
	}

	// Verify stage configs exist and are disabled
	stages := []struct {
		name   string
		config *StageConfig
	}{
		{"PreExecution", config.PreExecution},
		{"PreMerge", config.PreMerge},
		{"PostFailure", config.PostFailure},
	}

	for _, s := range stages {
		if s.config == nil {
			t.Errorf("expected %s config to be non-nil", s.name)
			continue
		}
		if s.config.Enabled {
			t.Errorf("expected %s to be disabled by default", s.name)
		}
		if s.config.DefaultAction != DecisionRejected {
			t.Errorf("expected %s default action to be rejected, got %s", s.name, s.config.DefaultAction)
		}
	}
}

func TestDecision_String(t *testing.T) {
	tests := []struct {
		decision Decision
		expected string
	}{
		{DecisionApproved, "approved"},
		{DecisionRejected, "rejected"},
		{DecisionTimeout, "timeout"},
	}

	for _, tt := range tests {
		if string(tt.decision) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, string(tt.decision))
		}
	}
}

func TestStage_String(t *testing.T) {
	tests := []struct {
		stage    Stage
		expected string
	}{
		{StagePreExecution, "pre_execution"},
		{StagePreMerge, "pre_merge"},
		{StagePostFailure, "post_failure"},
	}

	for _, tt := range tests {
		if string(tt.stage) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, string(tt.stage))
		}
	}
}

// --- Integration tests: rule evaluation wired into manager ---

func TestManager_NewManager_WiresRulesFromConfig(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Rules = []Rule{
		{
			Name:      "fail-gate",
			Condition: Condition{Type: ConditionConsecutiveFailures, Threshold: 3},
			Stage:     StagePreExecution,
			Enabled:   true,
		},
	}

	m := NewManager(config)

	// RuleEvaluator should be set automatically
	if m.ruleEvaluator == nil {
		t.Fatal("expected rule evaluator to be initialized from config rules")
	}
}

func TestManager_NewManager_NoRules_NoEvaluator(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	// No rules

	m := NewManager(config)

	if m.ruleEvaluator != nil {
		t.Error("expected no rule evaluator when config has no rules")
	}
}
