package alerts

import (
	"context"
	"testing"
	"time"
)

func TestEngine_CostUpdate_DailySpend(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "daily_spend",
				Type:    AlertTypeDailySpend,
				Enabled: true,
				Condition: RuleCondition{
					DailySpendThreshold: 50.0,
				},
				Severity: SeverityWarning,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	tests := []struct {
		name         string
		dailySpend   string
		expectAlerts int
	}{
		{
			name:         "below threshold",
			dailySpend:   "40.00",
			expectAlerts: 0,
		},
		{
			name:         "above threshold",
			dailySpend:   "60.00",
			expectAlerts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCh.mu.Lock()
			mockCh.alerts = make([]*Alert, 0)
			mockCh.mu.Unlock()

			engine.ProcessEvent(Event{
				Type:      EventTypeCostUpdate,
				Metadata:  map[string]string{"daily_spend": tt.dailySpend},
				Timestamp: time.Now(),
			})

			engine.flushForTest()
			alerts := mockCh.getAlerts()
			if len(alerts) != tt.expectAlerts {
				t.Errorf("expected %d alerts, got %d", tt.expectAlerts, len(alerts))
			}
		})
	}
}

func TestEngine_CostUpdate_BudgetDepleted(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "budget_depleted",
				Type:    AlertTypeBudgetDepleted,
				Enabled: true,
				Condition: RuleCondition{
					BudgetLimit: 500.0,
				},
				Severity: SeverityCritical,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	engine.ProcessEvent(Event{
		Type: EventTypeCostUpdate,
		Metadata: map[string]string{
			"daily_spend": "20.00",
			"total_spend": "550.00",
		},
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockCh, 1, 2*time.Second)
	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 budget depleted alert, got %d", len(alerts))
		return
	}

	if alerts[0].Type != AlertTypeBudgetDepleted {
		t.Errorf("expected alert type %s, got %s", AlertTypeBudgetDepleted, alerts[0].Type)
	}
}

// GH-539: Test that EventTypeBudgetExceeded routes through cost update rules
func TestEngine_BudgetExceeded_RoutesToCostRules(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "budget_depleted",
				Type:    AlertTypeBudgetDepleted,
				Enabled: true,
				Condition: RuleCondition{
					BudgetLimit: 500.0,
				},
				Severity: SeverityCritical,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	// Budget exceeded event should be routed through handleBudgetEvent → handleCostUpdate
	engine.ProcessEvent(Event{
		Type:   EventTypeBudgetExceeded,
		TaskID: "GH-100",
		Error:  "Daily budget exceeded: $55.00 / $50.00",
		Metadata: map[string]string{
			"daily_spend":  "55.00",
			"total_spend":  "600.00",
			"daily_left":   "0.00",
			"monthly_left": "0.00",
			"action":       "stop",
		},
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockCh, 1, 2*time.Second)
	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 budget depleted alert from BudgetExceeded event, got %d", len(alerts))
		return
	}

	if alerts[0].Type != AlertTypeBudgetDepleted {
		t.Errorf("expected alert type %s, got %s", AlertTypeBudgetDepleted, alerts[0].Type)
	}
}

// GH-539: Test that EventTypeBudgetWarning routes through cost update rules
func TestEngine_BudgetWarning_RoutesToCostRules(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "daily_spend_alert",
				Type:    AlertTypeDailySpend,
				Enabled: true,
				Condition: RuleCondition{
					DailySpendThreshold: 40.0,
				},
				Severity: SeverityWarning,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	// Budget warning event should route to daily spend rule
	engine.ProcessEvent(Event{
		Type:  EventTypeBudgetWarning,
		Error: "Daily budget at 90%: $45.00 / $50.00",
		Metadata: map[string]string{
			"daily_spend": "45.00",
			"alert_type":  "daily_budget_warning",
			"severity":    "warning",
		},
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockCh, 1, 2*time.Second)
	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 daily spend alert from BudgetWarning event, got %d", len(alerts))
		return
	}

	if alerts[0].Type != AlertTypeDailySpend {
		t.Errorf("expected alert type %s, got %s", AlertTypeDailySpend, alerts[0].Type)
	}
}

func TestEngine_SecurityEvent(t *testing.T) {
	tests := []struct {
		name      string
		alertType AlertType
		metadata  map[string]string
	}{
		{
			name:      "unauthorized access",
			alertType: AlertTypeUnauthorizedAccess,
			metadata:  map[string]string{"user": "unknown"},
		},
		{
			name:      "sensitive file modified",
			alertType: AlertTypeSensitiveFile,
			metadata:  map[string]string{"file_path": "/etc/passwd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AlertConfig{
				Enabled: true,
				Channels: []ChannelConfig{
					{Name: "test-channel", Type: "webhook", Enabled: true},
				},
				Rules: []AlertRule{
					{
						Name:     tt.name,
						Type:     tt.alertType,
						Enabled:  true,
						Severity: SeverityCritical,
						Channels: []string{"test-channel"},
						Cooldown: 0,
					},
				},
			}

			mockCh := newMockChannel("test-channel", "webhook")
			dispatcher := NewDispatcher(config)
			dispatcher.RegisterChannel(mockCh)

			engine := NewEngine(config, WithDispatcher(dispatcher))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_ = engine.Start(ctx)

			engine.ProcessEvent(Event{
				Type:      EventTypeSecurityEvent,
				Metadata:  tt.metadata,
				Timestamp: time.Now(),
			})

			waitForAlerts(t, mockCh, 1, 2*time.Second)
			alerts := mockCh.getAlerts()
			if len(alerts) != 1 {
				t.Errorf("expected 1 alert, got %d", len(alerts))
				return
			}

			if alerts[0].Type != tt.alertType {
				t.Errorf("expected alert type %s, got %s", tt.alertType, alerts[0].Type)
			}
		})
	}
}
