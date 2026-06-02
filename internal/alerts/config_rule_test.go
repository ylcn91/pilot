package alerts

import (
	"testing"
	"time"
)

func TestConvertRule(t *testing.T) {
	tests := []struct {
		name   string
		input  RuleConfigInput
		verify func(t *testing.T, rule AlertRule)
	}{
		{
			name: "basic rule",
			input: RuleConfigInput{
				Name:        "my-rule",
				Type:        "task_failed",
				Enabled:     true,
				Severity:    "critical",
				Channels:    []string{"slack", "telegram"},
				Cooldown:    10 * time.Minute,
				Description: "Test rule",
			},
			verify: func(t *testing.T, rule AlertRule) {
				if rule.Name != "my-rule" {
					t.Errorf("expected name 'my-rule', got '%s'", rule.Name)
				}
				if rule.Type != AlertTypeTaskFailed {
					t.Errorf("expected type TaskFailed, got %s", rule.Type)
				}
				if !rule.Enabled {
					t.Error("expected enabled to be true")
				}
				if rule.Severity != SeverityCritical {
					t.Errorf("expected severity Critical, got %s", rule.Severity)
				}
				if len(rule.Channels) != 2 {
					t.Errorf("expected 2 channels, got %d", len(rule.Channels))
				}
				if rule.Cooldown != 10*time.Minute {
					t.Errorf("expected cooldown 10m, got %v", rule.Cooldown)
				}
			},
		},
		{
			name: "rule with condition",
			input: RuleConfigInput{
				Name:    "stuck-task-rule",
				Type:    "task_stuck",
				Enabled: true,
				Condition: ConditionConfigInput{
					ProgressUnchangedFor: 15 * time.Minute,
				},
				Severity: "warning",
			},
			verify: func(t *testing.T, rule AlertRule) {
				if rule.Condition.ProgressUnchangedFor != 15*time.Minute {
					t.Errorf("expected ProgressUnchangedFor 15m, got %v", rule.Condition.ProgressUnchangedFor)
				}
			},
		},
		{
			name: "rule with all condition fields",
			input: RuleConfigInput{
				Name:    "complex-rule",
				Type:    "task_failed",
				Enabled: true,
				Condition: ConditionConfigInput{
					ProgressUnchangedFor: 20 * time.Minute,
					ConsecutiveFailures:  5,
					DailySpendThreshold:  100.0,
					BudgetLimit:          500.0,
					UsageSpikePercent:    300.0,
					Pattern:              "error.*",
					FilePattern:          "*.secret",
					Paths:                []string{"/etc/passwd"},
				},
				Severity: "critical",
			},
			verify: func(t *testing.T, rule AlertRule) {
				if rule.Condition.ConsecutiveFailures != 5 {
					t.Errorf("expected ConsecutiveFailures 5, got %d", rule.Condition.ConsecutiveFailures)
				}
				if rule.Condition.DailySpendThreshold != 100.0 {
					t.Errorf("expected DailySpendThreshold 100.0, got %f", rule.Condition.DailySpendThreshold)
				}
				if rule.Condition.BudgetLimit != 500.0 {
					t.Errorf("expected BudgetLimit 500.0, got %f", rule.Condition.BudgetLimit)
				}
				if rule.Condition.UsageSpikePercent != 300.0 {
					t.Errorf("expected UsageSpikePercent 300.0, got %f", rule.Condition.UsageSpikePercent)
				}
				if rule.Condition.Pattern != "error.*" {
					t.Errorf("expected Pattern 'error.*', got '%s'", rule.Condition.Pattern)
				}
				if rule.Condition.FilePattern != "*.secret" {
					t.Errorf("expected FilePattern '*.secret', got '%s'", rule.Condition.FilePattern)
				}
				if len(rule.Condition.Paths) != 1 {
					t.Errorf("expected 1 path, got %d", len(rule.Condition.Paths))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertRule(tt.input)
			tt.verify(t, result)
		})
	}
}

func TestConditionConfigInput_ZeroValues(t *testing.T) {
	input := RuleConfigInput{
		Name:      "zero-condition",
		Type:      "task_failed",
		Enabled:   true,
		Condition: ConditionConfigInput{}, // All zero values
		Severity:  "warning",
	}

	result := convertRule(input)

	if result.Condition.ProgressUnchangedFor != 0 {
		t.Errorf("expected ProgressUnchangedFor 0, got %v", result.Condition.ProgressUnchangedFor)
	}
	if result.Condition.ConsecutiveFailures != 0 {
		t.Errorf("expected ConsecutiveFailures 0, got %d", result.Condition.ConsecutiveFailures)
	}
	if result.Condition.DailySpendThreshold != 0 {
		t.Errorf("expected DailySpendThreshold 0, got %f", result.Condition.DailySpendThreshold)
	}
}
