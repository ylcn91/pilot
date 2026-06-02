package budget

import (
	"context"
	"testing"
)

func TestEnforcer_CheckBudget_Disabled(t *testing.T) {
	config := &Config{
		Enabled: false,
	}
	provider := &mockUsageProvider{}
	enforcer := NewEnforcer(config, provider)

	result, err := enforcer.CheckBudget(context.Background(), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("expected task to be allowed when budget is disabled")
	}
}

func TestEnforcer_CheckBudget_UnderLimits(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{
			WarnPercent: 80,
		},
	}
	provider := &mockUsageProvider{
		dailyCost:   10.0,  // 20% of limit
		monthlyCost: 100.0, // 20% of limit
	}
	enforcer := NewEnforcer(config, provider)

	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("expected task to be allowed under limits")
	}

	if result.DailyLeft != 40.0 {
		t.Errorf("expected daily left to be 40.0, got %v", result.DailyLeft)
	}

	if result.MonthlyLeft != 400.0 {
		t.Errorf("expected monthly left to be 400.0, got %v", result.MonthlyLeft)
	}
}

func TestEnforcer_CheckBudget_DailyLimitExceeded(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{
			WarnPercent: 80,
		},
	}
	provider := &mockUsageProvider{
		dailyCost:   55.0, // Over daily limit
		monthlyCost: 100.0,
	}
	enforcer := NewEnforcer(config, provider)

	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Allowed {
		t.Error("expected task to be blocked when daily limit exceeded")
	}

	if result.Action != ActionStop {
		t.Errorf("expected action to be stop, got %v", result.Action)
	}

	if result.DailyLeft != 0 {
		t.Errorf("expected daily left to be 0, got %v", result.DailyLeft)
	}
}

func TestEnforcer_CheckBudget_MonthlyLimitExceeded(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{
			WarnPercent: 80,
		},
	}
	provider := &mockUsageProvider{
		dailyCost:   10.0,
		monthlyCost: 550.0, // Over monthly limit
	}
	enforcer := NewEnforcer(config, provider)

	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Allowed {
		t.Error("expected task to be blocked when monthly limit exceeded")
	}

	if result.Action != ActionStop {
		t.Errorf("expected action to be stop, got %v", result.Action)
	}

	if result.MonthlyLeft != 0 {
		t.Errorf("expected monthly left to be 0, got %v", result.MonthlyLeft)
	}
}

func TestEnforcer_CheckBudget_WarnAction(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		OnExceed: ExceedAction{
			Daily:   ActionWarn, // Warn only, don't block
			Monthly: ActionWarn,
		},
		Thresholds: ThresholdConfig{
			WarnPercent: 80,
		},
	}
	provider := &mockUsageProvider{
		dailyCost:   55.0, // Over limit but action is warn
		monthlyCost: 100.0,
	}
	enforcer := NewEnforcer(config, provider)

	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Warn action should still allow tasks
	if !result.Allowed {
		t.Error("expected task to be allowed with warn action")
	}
}

func TestEnforcer_CheckBudget_PauseAction(t *testing.T) {
	tests := []struct {
		name        string
		dailyCost   float64
		monthlyCost float64
		onExceed    ExceedAction
		wantAllowed bool
		wantAction  Action
	}{
		{
			name:        "daily limit with pause action",
			dailyCost:   55.0,
			monthlyCost: 100.0,
			onExceed: ExceedAction{
				Daily:   ActionPause,
				Monthly: ActionStop,
			},
			wantAllowed: false,
			wantAction:  ActionPause,
		},
		{
			name:        "monthly limit with pause action",
			dailyCost:   10.0,
			monthlyCost: 550.0,
			onExceed: ExceedAction{
				Daily:   ActionStop,
				Monthly: ActionPause,
			},
			wantAllowed: false,
			wantAction:  ActionPause,
		},
		{
			name:        "monthly exceeds but daily has warn action",
			dailyCost:   55.0,
			monthlyCost: 550.0,
			onExceed: ExceedAction{
				Daily:   ActionWarn,
				Monthly: ActionStop,
			},
			wantAllowed: false,
			wantAction:  ActionStop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Enabled:      true,
				DailyLimit:   50.0,
				MonthlyLimit: 500.0,
				OnExceed:     tt.onExceed,
				Thresholds:   ThresholdConfig{WarnPercent: 80},
			}
			provider := &mockUsageProvider{
				dailyCost:   tt.dailyCost,
				monthlyCost: tt.monthlyCost,
			}
			enforcer := NewEnforcer(config, provider)

			result, err := enforcer.CheckBudget(context.Background(), "", "user1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v", result.Allowed, tt.wantAllowed)
			}
			if result.Action != tt.wantAction {
				t.Errorf("Action = %v, want %v", result.Action, tt.wantAction)
			}
		})
	}
}

func TestEnforcer_BlockedTasksCounter(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		OnExceed: ExceedAction{
			Daily:   ActionStop,
			Monthly: ActionStop,
		},
		Thresholds: ThresholdConfig{WarnPercent: 80},
	}
	provider := &mockUsageProvider{
		dailyCost:   55.0, // Over limit
		monthlyCost: 100.0,
	}
	enforcer := NewEnforcer(config, provider)

	// Make multiple budget checks that should be blocked
	for i := 0; i < 3; i++ {
		_, _ = enforcer.CheckBudget(context.Background(), "", "user1")
	}

	status, err := enforcer.GetStatus(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.BlockedTasks != 3 {
		t.Errorf("expected 3 blocked tasks, got %d", status.BlockedTasks)
	}
}

func TestEnforcer_CheckBudget_ProviderError(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
	}
	provider := &errorUsageProvider{}
	enforcer := NewEnforcer(config, provider)

	// On error, should still allow (fail-open)
	result, err := enforcer.CheckBudget(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("expected task allowed on provider error (fail-open)")
	}
}
