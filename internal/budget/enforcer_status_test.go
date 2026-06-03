package budget

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestDefaultConfig_BudgetDefaults(t *testing.T) {
	// GH-2163: Budget defaults updated for 1M context window
	cfg := DefaultConfig()
	if cfg.PerTask.MaxTokens != 500000 {
		t.Errorf("PerTask.MaxTokens = %d, want 500000", cfg.PerTask.MaxTokens)
	}
	if cfg.PerTask.MaxDuration != 60*time.Minute {
		t.Errorf("PerTask.MaxDuration = %v, want 60m", cfg.PerTask.MaxDuration)
	}
}

func TestEnforcer_GetStatus(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		Thresholds: ThresholdConfig{
			WarnPercent: 80,
		},
	}
	provider := &mockUsageProvider{
		dailyCost:   25.0,  // 50%
		monthlyCost: 250.0, // 50%
	}
	enforcer := NewEnforcer(config, provider)

	status, err := enforcer.GetStatus(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.DailySpent != 25.0 {
		t.Errorf("expected daily spent 25.0, got %v", status.DailySpent)
	}
	if status.DailyLimit != 50.0 {
		t.Errorf("expected daily limit 50.0, got %v", status.DailyLimit)
	}
	if status.DailyPercent != 50.0 {
		t.Errorf("expected daily percent 50.0, got %v", status.DailyPercent)
	}

	if status.MonthlySpent != 250.0 {
		t.Errorf("expected monthly spent 250.0, got %v", status.MonthlySpent)
	}
	if status.MonthlyLimit != 500.0 {
		t.Errorf("expected monthly limit 500.0, got %v", status.MonthlyLimit)
	}
	if status.MonthlyPercent != 50.0 {
		t.Errorf("expected monthly percent 50.0, got %v", status.MonthlyPercent)
	}

	if status.IsExceeded() {
		t.Error("expected not exceeded at 50%")
	}
	if status.IsWarning(80) {
		t.Error("expected no warning at 50%")
	}
}

func TestEnforcer_GetStatus_Warning(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		Thresholds: ThresholdConfig{
			WarnPercent: 80,
		},
	}
	provider := &mockUsageProvider{
		dailyCost:   45.0,  // 90%
		monthlyCost: 250.0, // 50%
	}
	enforcer := NewEnforcer(config, provider)

	status, err := enforcer.GetStatus(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.IsWarning(80) {
		t.Error("expected warning at 90% daily")
	}
}

func TestEnforcer_GetStatus_ProviderError(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
	}
	provider := &errorUsageProvider{}
	enforcer := NewEnforcer(config, provider)

	_, err := enforcer.GetStatus(context.Background(), "", "user1")
	if err == nil {
		t.Error("expected error from GetStatus when provider fails")
	}
}

func TestEnforcer_GetPerTaskLimits(t *testing.T) {
	tests := []struct {
		name            string
		config          *Config
		wantMaxTokens   int64
		wantMaxDuration time.Duration
	}{
		{
			name: "disabled returns zero limits",
			config: &Config{
				Enabled: false,
				PerTask: PerTaskConfig{
					MaxTokens:   100000,
					MaxDuration: 30 * time.Minute,
				},
			},
			wantMaxTokens:   0,
			wantMaxDuration: 0,
		},
		{
			name: "enabled returns configured limits",
			config: &Config{
				Enabled: true,
				PerTask: PerTaskConfig{
					MaxTokens:   50000,
					MaxDuration: 15 * time.Minute,
				},
			},
			wantMaxTokens:   50000,
			wantMaxDuration: 15 * time.Minute,
		},
		{
			name: "enabled with zero values returns zeros",
			config: &Config{
				Enabled: true,
				PerTask: PerTaskConfig{
					MaxTokens:   0,
					MaxDuration: 0,
				},
			},
			wantMaxTokens:   0,
			wantMaxDuration: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enforcer := NewEnforcer(tt.config, &mockUsageProvider{})
			gotTokens, gotDuration := enforcer.GetPerTaskLimits()
			if gotTokens != tt.wantMaxTokens {
				t.Errorf("GetPerTaskLimits() maxTokens = %v, want %v", gotTokens, tt.wantMaxTokens)
			}
			if gotDuration != tt.wantMaxDuration {
				t.Errorf("GetPerTaskLimits() maxDuration = %v, want %v", gotDuration, tt.wantMaxDuration)
			}
		})
	}
}

func TestEnforcer_UpdateConfig(t *testing.T) {
	initialConfig := &Config{
		Enabled:      false,
		DailyLimit:   10.0,
		MonthlyLimit: 100.0,
	}
	enforcer := NewEnforcer(initialConfig, &mockUsageProvider{})

	newConfig := &Config{
		Enabled:      true,
		DailyLimit:   50.0,
		MonthlyLimit: 500.0,
		PerTask: PerTaskConfig{
			MaxTokens:   100000,
			MaxDuration: 30 * time.Minute,
		},
	}

	enforcer.UpdateConfig(newConfig)

	gotConfig := enforcer.GetConfig()
	if gotConfig.Enabled != true {
		t.Error("expected enabled after update")
	}
	if gotConfig.DailyLimit != 50.0 {
		t.Errorf("expected daily limit 50.0, got %v", gotConfig.DailyLimit)
	}
	if gotConfig.MonthlyLimit != 500.0 {
		t.Errorf("expected monthly limit 500.0, got %v", gotConfig.MonthlyLimit)
	}
}

func TestEnforcer_GetConfig(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   75.0,
		MonthlyLimit: 750.0,
		PerTask: PerTaskConfig{
			MaxTokens:   200000,
			MaxDuration: 45 * time.Minute,
		},
		OnExceed: ExceedAction{
			Daily:   ActionWarn,
			Monthly: ActionPause,
			PerTask: ActionStop,
		},
		Thresholds: ThresholdConfig{
			WarnPercent: 85,
		},
	}
	enforcer := NewEnforcer(config, &mockUsageProvider{})

	got := enforcer.GetConfig()

	if got != config {
		t.Error("GetConfig should return the same config reference")
	}
}

func TestPercentOf(t *testing.T) {
	tests := []struct {
		name  string
		spent float64
		limit float64
		want  float64
	}{
		{"half", 25, 50, 50},
		{"full", 50, 50, 100},
		{"zero limit disabled", 25, 0, 0},
		{"negative limit disabled", 25, -1, 0},
		{"zero spent", 0, 50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentOf(tt.spent, tt.limit); got != tt.want {
				t.Errorf("percentOf(%v, %v) = %v, want %v", tt.spent, tt.limit, got, tt.want)
			}
		})
	}
}

// TestEnforcer_GetStatus_ZeroLimits verifies that a disabled/misconfigured
// limit (0.0) yields 0 percent instead of NaN/Inf, which would otherwise
// poison alert-threshold comparisons and the dashboard/API.
func TestEnforcer_GetStatus_ZeroLimits(t *testing.T) {
	config := &Config{
		Enabled:      true,
		DailyLimit:   0.0,
		MonthlyLimit: 0.0,
	}
	provider := &mockUsageProvider{
		dailyCost:   10.0,
		monthlyCost: 100.0,
	}
	enforcer := NewEnforcer(config, provider)

	status, err := enforcer.GetStatus(context.Background(), "", "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if math.IsNaN(status.DailyPercent) || math.IsInf(status.DailyPercent, 0) {
		t.Errorf("DailyPercent is NaN/Inf: %v", status.DailyPercent)
	}
	if math.IsNaN(status.MonthlyPercent) || math.IsInf(status.MonthlyPercent, 0) {
		t.Errorf("MonthlyPercent is NaN/Inf: %v", status.MonthlyPercent)
	}
	if status.DailyPercent != 0 {
		t.Errorf("DailyPercent = %v, want 0 for disabled limit", status.DailyPercent)
	}
	if status.MonthlyPercent != 0 {
		t.Errorf("MonthlyPercent = %v, want 0 for disabled limit", status.MonthlyPercent)
	}
}
