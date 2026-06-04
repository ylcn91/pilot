package alerts

import (
	"context"
	"strings"
	"testing"
	"time"
)

// autopilotRule builds an enabled rule of the given type routed to the named
// channel with no cooldown, so each ProcessEvent fires deterministically.
func autopilotRule(name string, typ AlertType, sev Severity, channel string, cond RuleCondition) AlertRule {
	return AlertRule{
		Name:      name,
		Type:      typ,
		Enabled:   true,
		Condition: cond,
		Severity:  sev,
		Channels:  []string{channel},
		Cooldown:  0,
	}
}

// mkAutopilotEngine wires a started engine with one mock channel and the full
// set of autopilot rules so a single EventTypeAutopilotMetrics event can be
// evaluated against every autopilot rule type.
func mkAutopilotEngine(t *testing.T) (*Engine, *mockChannel) {
	t.Helper()
	mock := newMockChannel("mock", "webhook")
	config := &AlertConfig{
		Enabled:  true,
		Channels: []ChannelConfig{{Name: mock.Name(), Type: mock.Type(), Enabled: true}},
		Rules: []AlertRule{
			autopilotRule("failed_queue_high", AlertTypeFailedQueueHigh, SeverityWarning, mock.Name(),
				RuleCondition{FailedQueueThreshold: 5}),
			autopilotRule("circuit_breaker_trip", AlertTypeCircuitBreakerTrip, SeverityCritical, mock.Name(),
				RuleCondition{}),
			autopilotRule("api_error_rate_high", AlertTypeAPIErrorRateHigh, SeverityWarning, mock.Name(),
				RuleCondition{APIErrorRatePerMin: 10.0}),
			autopilotRule("pr_stuck_waiting_ci", AlertTypePRStuckWaitingCI, SeverityInfo, mock.Name(),
				RuleCondition{PRStuckTimeout: 15 * time.Minute}),
			autopilotRule("autopilot_deadlock", AlertTypeDeadlock, SeverityCritical, mock.Name(),
				RuleCondition{DeadlockTimeout: 1 * time.Hour}),
		},
		// Suppression off: each rule fires a distinct alert; we assert exact counts.
		Defaults: AlertDefaults{SuppressDuplicates: false},
	}
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mock)
	return NewEngine(config, WithDispatcher(dispatcher)), mock
}

// TestEngine_AutopilotMetrics_AllRulesFire verifies that a single
// EventTypeAutopilotMetrics event whose metadata crosses every autopilot
// threshold fires one alert per autopilot rule type (FailedQueueHigh,
// CircuitBreakerTrip, APIErrorRateHigh, PRStuckWaitingCI, Deadlock).
func TestEngine_AutopilotMetrics_AllRulesFire(t *testing.T) {
	engine, mock := mkAutopilotEngine(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}
	defer engine.Stop()

	engine.ProcessEvent(Event{
		Type:      EventTypeAutopilotMetrics,
		Project:   "/test/project",
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"failed_queue_depth":    "8",     // >= 5 threshold
			"circuit_breaker_trips": "2",     // > 0
			"api_error_rate":        "15.0",  // >= 10/min
			"pr_stuck_count":        "3",     // > 0
			"pr_max_wait_minutes":   "30",    // >= 15 min
			"no_progress_minutes":   "120",   // >= 60 min (1h deadlock timeout)
			"deadlock_alert_sent":   "false", // not yet alerted
			"last_known_state":      "waiting_ci",
			"last_known_pr":         "42",
		},
	})

	waitForAlerts(t, mock, 5, 2*time.Second)
	engine.flushForTest()

	alerts := mock.getAlerts()
	if len(alerts) != 5 {
		t.Fatalf("expected 5 alerts (one per autopilot rule), got %d", len(alerts))
	}

	gotTypes := make(map[AlertType]int)
	for _, a := range alerts {
		gotTypes[a.Type]++
	}

	wantTypes := []AlertType{
		AlertTypeFailedQueueHigh,
		AlertTypeCircuitBreakerTrip,
		AlertTypeAPIErrorRateHigh,
		AlertTypePRStuckWaitingCI,
		AlertTypeDeadlock,
	}
	for _, wt := range wantTypes {
		if gotTypes[wt] != 1 {
			t.Errorf("expected exactly 1 alert of type %s, got %d", wt, gotTypes[wt])
		}
	}
}

// TestEngine_AutopilotMetrics_BelowThresholds_NoAlerts verifies that an
// autopilot metrics event whose values stay below every threshold fires no
// alerts (guards against off-by-one / always-fire regressions).
func TestEngine_AutopilotMetrics_BelowThresholds_NoAlerts(t *testing.T) {
	engine, mock := mkAutopilotEngine(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}
	defer engine.Stop()

	engine.ProcessEvent(Event{
		Type:      EventTypeAutopilotMetrics,
		Project:   "/test/project",
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"failed_queue_depth":    "1",   // < 5
			"circuit_breaker_trips": "0",   // not > 0
			"api_error_rate":        "2.0", // < 10
			"pr_stuck_count":        "0",   // not > 0
			"pr_max_wait_minutes":   "1",   // < 15
			"no_progress_minutes":   "5",   // < 60
			"deadlock_alert_sent":   "false",
		},
	})

	engine.flushForTest()

	if got := len(mock.getAlerts()); got != 0 {
		t.Fatalf("expected 0 alerts below thresholds, got %d", got)
	}
}

// TestEngine_AutopilotMetrics_APIErrorRateHighFires verifies the
// api_error_rate_high rule fires once a high api_error_rate metric crosses its
// per-minute threshold. The rule is default-on but in production never fired
// because the producer always reported 0 (the metric is wired separately).
// This test pins the rule's own correctness: given a high rate it fires with a
// message naming both the observed rate and the threshold.
func TestEngine_AutopilotMetrics_APIErrorRateHighFires(t *testing.T) {
	mock := newMockChannel("mock", "webhook")
	config := &AlertConfig{
		Enabled:  true,
		Channels: []ChannelConfig{{Name: mock.Name(), Type: mock.Type(), Enabled: true}},
		Rules: []AlertRule{
			autopilotRule("api_error_rate_high", AlertTypeAPIErrorRateHigh, SeverityWarning, mock.Name(),
				RuleCondition{APIErrorRatePerMin: 10.0}),
		},
		Defaults: AlertDefaults{SuppressDuplicates: false},
	}
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mock)
	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}
	defer engine.Stop()

	engine.ProcessEvent(Event{
		Type:      EventTypeAutopilotMetrics,
		Project:   "/test/project",
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"api_error_rate": "25.0", // >= 10/min threshold
		},
	})

	waitForAlerts(t, mock, 1, 2*time.Second)
	engine.flushForTest()

	alerts := mock.getAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 api_error_rate_high alert, got %d", len(alerts))
	}
	if alerts[0].Type != AlertTypeAPIErrorRateHigh {
		t.Errorf("expected alert type %s, got %s", AlertTypeAPIErrorRateHigh, alerts[0].Type)
	}
	if !strings.Contains(alerts[0].Message, "25.0") || !strings.Contains(alerts[0].Message, "10.0") {
		t.Errorf("expected message to name observed rate and threshold, got %q", alerts[0].Message)
	}
}

// TestEngine_AutopilotMetrics_APIErrorRateBelowThreshold_NoAlert verifies the
// api_error_rate_high rule stays silent when the rate is below threshold,
// including the production-default value of 0 that the producer reports until
// the metric is wired.
func TestEngine_AutopilotMetrics_APIErrorRateBelowThreshold_NoAlert(t *testing.T) {
	mock := newMockChannel("mock", "webhook")
	config := &AlertConfig{
		Enabled:  true,
		Channels: []ChannelConfig{{Name: mock.Name(), Type: mock.Type(), Enabled: true}},
		Rules: []AlertRule{
			autopilotRule("api_error_rate_high", AlertTypeAPIErrorRateHigh, SeverityWarning, mock.Name(),
				RuleCondition{APIErrorRatePerMin: 10.0}),
		},
		Defaults: AlertDefaults{SuppressDuplicates: false},
	}
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mock)
	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}
	defer engine.Stop()

	for _, rate := range []string{"0", "9.9"} {
		engine.ProcessEvent(Event{
			Type:      EventTypeAutopilotMetrics,
			Project:   "/test/project",
			Timestamp: time.Now(),
			Metadata:  map[string]string{"api_error_rate": rate},
		})
	}

	engine.flushForTest()

	if got := len(mock.getAlerts()); got != 0 {
		t.Fatalf("expected 0 alerts below threshold, got %d", got)
	}
}

// TestEngine_AutopilotMetrics_DeadlockSuppressedWhenAlreadySent verifies the
// deadlock rule does not re-fire when deadlock_alert_sent is already "true",
// even though no_progress_minutes exceeds the timeout.
func TestEngine_AutopilotMetrics_DeadlockSuppressedWhenAlreadySent(t *testing.T) {
	mock := newMockChannel("mock", "webhook")
	config := &AlertConfig{
		Enabled:  true,
		Channels: []ChannelConfig{{Name: mock.Name(), Type: mock.Type(), Enabled: true}},
		Rules: []AlertRule{
			autopilotRule("autopilot_deadlock", AlertTypeDeadlock, SeverityCritical, mock.Name(),
				RuleCondition{DeadlockTimeout: 1 * time.Hour}),
		},
		Defaults: AlertDefaults{SuppressDuplicates: false},
	}
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mock)
	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}
	defer engine.Stop()

	engine.ProcessEvent(Event{
		Type:      EventTypeAutopilotMetrics,
		Project:   "/test/project",
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"no_progress_minutes": "120",  // >= 60
			"deadlock_alert_sent": "true", // already alerted → must not re-fire
		},
	})

	engine.flushForTest()

	if got := len(mock.getAlerts()); got != 0 {
		t.Fatalf("expected 0 deadlock alerts when alert already sent, got %d", got)
	}
}
