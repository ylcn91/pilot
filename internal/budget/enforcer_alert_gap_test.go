package budget

import (
	"context"
	"sync"
	"testing"
)

// capturedAlert records the arguments passed to an OnAlert callback.
type capturedAlert struct {
	alertType string
	message   string
	severity  string
}

// TestCheckBudgetFiresWarningAlert covers the warning-threshold branch in
// CheckBudget -> fireAlert: when daily spend is in the warning band
// (>= WarnPercent and < 100) the daily_budget_warning alert must fire while
// the task is still allowed.
func TestCheckBudgetFiresWarningAlert(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   100.0,
		MonthlyLimit: 1000.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{WarnPercent: 80},
	}
	provider := &mockUsageProvider{
		dailyCost:   85.0,  // 85% -> warning band
		monthlyCost: 200.0, // 20% -> no alert
	}
	enforcer := NewEnforcer(config, provider)

	var mu sync.Mutex
	var alerts []capturedAlert
	enforcer.OnAlert(func(alertType, message, severity string) {
		mu.Lock()
		alerts = append(alerts, capturedAlert{alertType, message, severity})
		mu.Unlock()
	})

	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("CheckBudget: %v", err)
	}
	if !result.Allowed {
		t.Error("task should still be allowed in the warning band")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(alerts) != 1 {
		t.Fatalf("expected exactly one warning alert, got %d: %+v", len(alerts), alerts)
	}
	got := alerts[0]
	if got.alertType != "daily_budget_warning" {
		t.Errorf("alertType = %q, want daily_budget_warning", got.alertType)
	}
	if got.severity != "warning" {
		t.Errorf("severity = %q, want warning", got.severity)
	}
	if got.message == "" {
		t.Error("expected a non-empty warning message")
	}
}

// TestCheckBudgetFiresBothWarningAlerts verifies that when both daily and
// monthly are in the warning band, both warning alerts fire.
func TestCheckBudgetFiresBothWarningAlerts(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   100.0,
		MonthlyLimit: 1000.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{WarnPercent: 80},
	}
	provider := &mockUsageProvider{
		dailyCost:   90.0,  // 90% warning
		monthlyCost: 850.0, // 85% warning
	}
	enforcer := NewEnforcer(config, provider)

	var mu sync.Mutex
	types := map[string]bool{}
	enforcer.OnAlert(func(alertType, message, severity string) {
		mu.Lock()
		types[alertType] = true
		mu.Unlock()
	})

	if _, err := enforcer.CheckBudget(context.Background(), "", "user1"); err != nil {
		t.Fatalf("CheckBudget: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !types["daily_budget_warning"] {
		t.Error("expected daily_budget_warning to fire")
	}
	if !types["monthly_budget_warning"] {
		t.Error("expected monthly_budget_warning to fire")
	}
}

// TestCheckBudgetNoAlertBelowThreshold verifies fireAlert is not reached when
// usage is below the warning threshold.
func TestCheckBudgetNoAlertBelowThreshold(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   100.0,
		MonthlyLimit: 1000.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{WarnPercent: 80},
	}
	provider := &mockUsageProvider{
		dailyCost:   50.0,  // 50%
		monthlyCost: 100.0, // 10%
	}
	enforcer := NewEnforcer(config, provider)

	var fired int
	enforcer.OnAlert(func(alertType, message, severity string) {
		fired++
	})

	if _, err := enforcer.CheckBudget(context.Background(), "", "user1"); err != nil {
		t.Fatalf("CheckBudget: %v", err)
	}
	if fired != 0 {
		t.Errorf("expected no alert below the warning threshold, got %d", fired)
	}
}
