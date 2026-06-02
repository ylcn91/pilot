package alerts

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	// Check defaults
	if cfg.Enabled {
		t.Error("expected Enabled to be false by default")
	}

	if len(cfg.Channels) != 0 {
		t.Errorf("expected empty channels, got %d", len(cfg.Channels))
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected default rules to be set")
	}

	// Check default rules
	if cfg.Defaults.Cooldown != 5*time.Minute {
		t.Errorf("expected default cooldown 5m, got %v", cfg.Defaults.Cooldown)
	}

	if cfg.Defaults.DefaultSeverity != SeverityWarning {
		t.Errorf("expected default severity Warning, got %v", cfg.Defaults.DefaultSeverity)
	}

	if !cfg.Defaults.SuppressDuplicates {
		t.Error("expected SuppressDuplicates to be true by default")
	}
}

func TestDefaultRules(t *testing.T) {
	rules := defaultRules()

	expectedRules := map[AlertType]struct {
		name    string
		enabled bool
	}{
		AlertTypeTaskStuck:        {"task_stuck", true},
		AlertTypeTaskFailed:       {"task_failed", true},
		AlertTypeConsecutiveFails: {"consecutive_failures", true},
		AlertTypeDailySpend:       {"daily_spend", false},
		AlertTypeBudgetDepleted:   {"budget_depleted", false},
		// Autopilot health rules (GH-728)
		AlertTypeFailedQueueHigh:    {"failed_queue_high", true},
		AlertTypeCircuitBreakerTrip: {"circuit_breaker_trip", true},
		AlertTypeAPIErrorRateHigh:   {"api_error_rate_high", true},
		AlertTypePRStuckWaitingCI:   {"pr_stuck_waiting_ci", true},
		// Deadlock detection (GH-849)
		AlertTypeDeadlock: {"autopilot_deadlock", true},
		// Escalation (GH-848)
		AlertTypeEscalation: {"escalation", true},
	}

	if len(rules) != len(expectedRules) {
		t.Errorf("expected %d default rules, got %d", len(expectedRules), len(rules))
	}

	for _, rule := range rules {
		expected, ok := expectedRules[rule.Type]
		if !ok {
			t.Errorf("unexpected rule type: %s", rule.Type)
			continue
		}

		if rule.Name != expected.name {
			t.Errorf("rule %s: expected name '%s', got '%s'", rule.Type, expected.name, rule.Name)
		}

		if rule.Enabled != expected.enabled {
			t.Errorf("rule %s: expected enabled=%v, got %v", rule.Type, expected.enabled, rule.Enabled)
		}
	}
}

func TestDefaultRules_TaskStuck(t *testing.T) {
	rules := defaultRules()

	var stuckRule *AlertRule
	for i := range rules {
		if rules[i].Type == AlertTypeTaskStuck {
			stuckRule = &rules[i]
			break
		}
	}

	if stuckRule == nil {
		t.Fatal("task_stuck rule not found")
	}

	if stuckRule.Condition.ProgressUnchangedFor != 10*time.Minute {
		t.Errorf("expected ProgressUnchangedFor 10m, got %v", stuckRule.Condition.ProgressUnchangedFor)
	}

	if stuckRule.Severity != SeverityWarning {
		t.Errorf("expected severity Warning, got %s", stuckRule.Severity)
	}

	if stuckRule.Cooldown != 15*time.Minute {
		t.Errorf("expected cooldown 15m, got %v", stuckRule.Cooldown)
	}
}

func TestDefaultRules_ConsecutiveFailures(t *testing.T) {
	rules := defaultRules()

	var failRule *AlertRule
	for i := range rules {
		if rules[i].Type == AlertTypeConsecutiveFails {
			failRule = &rules[i]
			break
		}
	}

	if failRule == nil {
		t.Fatal("consecutive_failures rule not found")
	}

	if failRule.Condition.ConsecutiveFailures != 3 {
		t.Errorf("expected ConsecutiveFailures 3, got %d", failRule.Condition.ConsecutiveFailures)
	}

	if failRule.Severity != SeverityCritical {
		t.Errorf("expected severity Critical, got %s", failRule.Severity)
	}

	if failRule.Cooldown != 30*time.Minute {
		t.Errorf("expected cooldown 30m, got %v", failRule.Cooldown)
	}
}

func TestDefaultRules_DailySpend(t *testing.T) {
	rules := defaultRules()

	var spendRule *AlertRule
	for i := range rules {
		if rules[i].Type == AlertTypeDailySpend {
			spendRule = &rules[i]
			break
		}
	}

	if spendRule == nil {
		t.Fatal("daily_spend rule not found")
	}

	if spendRule.Condition.DailySpendThreshold != 50.0 {
		t.Errorf("expected DailySpendThreshold 50.0, got %f", spendRule.Condition.DailySpendThreshold)
	}

	if spendRule.Enabled {
		t.Error("expected daily_spend rule to be disabled by default")
	}
}

func TestDefaultRules_BudgetDepleted(t *testing.T) {
	rules := defaultRules()

	var budgetRule *AlertRule
	for i := range rules {
		if rules[i].Type == AlertTypeBudgetDepleted {
			budgetRule = &rules[i]
			break
		}
	}

	if budgetRule == nil {
		t.Fatal("budget_depleted rule not found")
	}

	if budgetRule.Condition.BudgetLimit != 500.0 {
		t.Errorf("expected BudgetLimit 500.0, got %f", budgetRule.Condition.BudgetLimit)
	}

	if budgetRule.Enabled {
		t.Error("expected budget_depleted rule to be disabled by default")
	}
}
