package budget

import (
	"context"
	"testing"
)

func TestEnforcer_Pause_Resume(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
	}
	provider := &mockUsageProvider{
		dailyCost:   10.0,
		monthlyCost: 100.0,
	}
	enforcer := NewEnforcer(config, provider)

	// Initially not paused
	if enforcer.IsPaused() {
		t.Error("expected not paused initially")
	}

	// Pause
	enforcer.Pause("manual pause")
	if !enforcer.IsPaused() {
		t.Error("expected paused after Pause()")
	}

	// Check budget should fail when paused
	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected task blocked when paused")
	}
	if result.Action != ActionPause {
		t.Errorf("expected action pause, got %v", result.Action)
	}

	// Resume
	enforcer.Resume()
	if enforcer.IsPaused() {
		t.Error("expected not paused after Resume()")
	}

	// Check budget should work after resume
	result, err = enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected task allowed after resume")
	}
}

func TestEnforcer_ResetDaily(t *testing.T) {
	tests := []struct {
		name            string
		setupPause      bool
		pauseReason     string
		expectedResumed bool
		expectedBlocked int
	}{
		{
			name:            "resets blocked counter when not paused",
			setupPause:      false,
			pauseReason:     "",
			expectedResumed: true,
			expectedBlocked: 0,
		},
		{
			name:            "resumes if paused due to daily budget exceeded",
			setupPause:      true,
			pauseReason:     "Daily budget exceeded",
			expectedResumed: true,
			expectedBlocked: 0,
		},
		{
			name:            "stays paused if paused for other reason",
			setupPause:      true,
			pauseReason:     "Monthly budget exceeded",
			expectedResumed: false,
			expectedBlocked: 0,
		},
		{
			name:            "stays paused if paused manually",
			setupPause:      true,
			pauseReason:     "manual pause",
			expectedResumed: false,
			expectedBlocked: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Enabled:      true,
				DailyLimit:   50.0,
				MonthlyLimit: 500.0,
			}
			enforcer := NewEnforcer(config, &mockUsageProvider{})

			if tt.setupPause {
				enforcer.Pause(tt.pauseReason)
			}

			// Simulate some blocked tasks
			enforcer.mu.Lock()
			enforcer.blockedTasks = 5
			enforcer.mu.Unlock()

			enforcer.ResetDaily()

			enforcer.mu.RLock()
			gotBlocked := enforcer.blockedTasks
			isPaused := enforcer.paused
			enforcer.mu.RUnlock()

			if gotBlocked != tt.expectedBlocked {
				t.Errorf("ResetDaily() blockedTasks = %v, want %v", gotBlocked, tt.expectedBlocked)
			}
			if tt.expectedResumed && isPaused {
				t.Errorf("ResetDaily() expected to resume but still paused")
			}
			if !tt.expectedResumed && !isPaused {
				t.Errorf("ResetDaily() expected to stay paused but resumed")
			}
		})
	}
}

func TestEnforcer_OnAlert(t *testing.T) {
	tests := []struct {
		name         string
		dailyCost    float64
		monthlyCost  float64
		warnPercent  float64
		expectAlerts []string
	}{
		{
			name:         "daily warning alert",
			dailyCost:    45.0, // 90% of 50
			monthlyCost:  100.0,
			warnPercent:  80,
			expectAlerts: []string{"daily_budget_warning"},
		},
		{
			name:         "monthly warning alert",
			dailyCost:    10.0,
			monthlyCost:  450.0, // 90% of 500
			warnPercent:  80,
			expectAlerts: []string{"monthly_budget_warning"},
		},
		{
			name:         "both daily and monthly warning",
			dailyCost:    45.0,  // 90%
			monthlyCost:  450.0, // 90%
			warnPercent:  80,
			expectAlerts: []string{"daily_budget_warning", "monthly_budget_warning"},
		},
		{
			name:         "daily exceeded alert",
			dailyCost:    55.0, // 110%
			monthlyCost:  100.0,
			warnPercent:  80,
			expectAlerts: []string{"daily_budget_exceeded"},
		},
		{
			name:         "monthly exceeded alert",
			dailyCost:    10.0,
			monthlyCost:  550.0, // 110%
			warnPercent:  80,
			expectAlerts: []string{"monthly_budget_exceeded"},
		},
		{
			name:         "both exceeded alerts",
			dailyCost:    55.0,  // 110%
			monthlyCost:  550.0, // 110%
			warnPercent:  80,
			expectAlerts: []string{"daily_budget_exceeded", "monthly_budget_exceeded"},
		},
		{
			name:         "no alert below warning threshold",
			dailyCost:    30.0, // 60%
			monthlyCost:  300.0,
			warnPercent:  80,
			expectAlerts: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Enabled:      true,
				DailyLimit:   50.0,
				MonthlyLimit: 500.0,
				OnExceed: ExceedAction{
					Daily:   ActionWarn, // Use warn to allow execution
					Monthly: ActionWarn,
				},
				Thresholds: ThresholdConfig{
					WarnPercent: tt.warnPercent,
				},
			}
			provider := &mockUsageProvider{
				dailyCost:   tt.dailyCost,
				monthlyCost: tt.monthlyCost,
			}
			enforcer := NewEnforcer(config, provider)

			var receivedAlerts []string
			enforcer.OnAlert(func(alertType, message, severity string) {
				receivedAlerts = append(receivedAlerts, alertType)
			})

			// Trigger budget check which fires alerts
			_, _ = enforcer.CheckBudget(context.Background(), "", "user1")

			// Verify expected alerts
			if len(receivedAlerts) != len(tt.expectAlerts) {
				t.Errorf("expected %d alerts, got %d: %v", len(tt.expectAlerts), len(receivedAlerts), receivedAlerts)
				return
			}

			for i, expected := range tt.expectAlerts {
				if receivedAlerts[i] != expected {
					t.Errorf("alert[%d] = %v, want %v", i, receivedAlerts[i], expected)
				}
			}
		})
	}
}
