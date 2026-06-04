package autopilot

import (
	"context"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/alerts"
)

// #34: an open per-PR circuit breaker must drive the escalation hook (not just
// the inert Prometheus counter), so persistent trips reach PagerDuty.
func TestController_CircuitBreakerTripHook_Invoked(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFailures = 3
	cfg.FailureResetTimeout = time.Hour

	c := NewController(cfg, nil, nil, "owner", "repo")

	// Register an open breaker for PR 7 (FailureCount >= MaxFailures, recent).
	c.prFailures[7] = &prFailureState{FailureCount: 3, LastFailureTime: time.Now()}
	c.activePRs[7] = &PRState{PRNumber: 7, Stage: StageWaitingCI}

	var gotPR int
	var gotReason string
	called := 0
	c.SetCircuitBreakerTripHook(func(prNumber int, reason string) {
		called++
		gotPR = prNumber
		gotReason = reason
	})

	err := c.ProcessPR(context.Background(), 7, nil)
	if err == nil {
		t.Fatal("expected circuit-breaker error from ProcessPR")
	}
	if called != 1 {
		t.Fatalf("trip hook called %d times, want 1", called)
	}
	if gotPR != 7 {
		t.Errorf("hook prNumber = %d, want 7", gotPR)
	}
	if gotReason == "" {
		t.Error("hook reason should be non-empty")
	}
}

// AttachToController wires the alerter's escalation as the controller hook, and a
// trip escalates to PagerDuty once the alerter's threshold is crossed.
func TestController_BreakerTrip_Escalates(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFailures = 3
	cfg.FailureResetTimeout = time.Hour

	c := NewController(cfg, nil, nil, "owner", "repo")

	alertCfg := alerts.DefaultConfig()
	alertCfg.Enabled = true
	engine := alerts.NewEngine(alertCfg)
	ma := NewMetricsAlerter(c, engine)
	ma.AttachToController(c)

	// Open the breaker for PR 9.
	c.prFailures[9] = &prFailureState{FailureCount: 3, LastFailureTime: time.Now()}
	c.activePRs[9] = &PRState{PRNumber: 9, Stage: StageWaitingCI}

	ctx := context.Background()
	// Three trips cross the alerter's escalation threshold (3/hour).
	for i := 0; i < 3; i++ {
		if err := c.ProcessPR(ctx, 9, nil); err == nil {
			t.Fatalf("iteration %d: expected circuit-breaker error", i)
		}
	}

	if ma.tripTracker.lastEscalationSentAt.IsZero() {
		t.Error("escalation should have been sent after 3 breaker trips")
	}
}
