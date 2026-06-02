package alerts

import (
	"context"
	"fmt"
	"time"
)

// handleAutopilotMetrics evaluates autopilot health metrics against alert rules.
// Metadata keys: "failed_queue_depth", "circuit_breaker_trips", "api_error_rate",
// "pr_stuck_count", "pr_max_wait_minutes".
func (e *Engine) handleAutopilotMetrics(ctx context.Context, event Event) {
	failedQueueDepth := 0
	if v, ok := event.Metadata["failed_queue_depth"]; ok {
		_, _ = fmt.Sscanf(v, "%d", &failedQueueDepth)
	}

	cbTrips := 0
	if v, ok := event.Metadata["circuit_breaker_trips"]; ok {
		_, _ = fmt.Sscanf(v, "%d", &cbTrips)
	}

	apiErrorRate := 0.0
	if v, ok := event.Metadata["api_error_rate"]; ok {
		_, _ = fmt.Sscanf(v, "%f", &apiErrorRate)
	}

	prStuckCount := 0
	if v, ok := event.Metadata["pr_stuck_count"]; ok {
		_, _ = fmt.Sscanf(v, "%d", &prStuckCount)
	}

	prMaxWaitMin := 0.0
	if v, ok := event.Metadata["pr_max_wait_minutes"]; ok {
		_, _ = fmt.Sscanf(v, "%f", &prMaxWaitMin)
	}

	for _, rule := range e.config.Rules {
		if !rule.Enabled {
			continue
		}

		switch rule.Type {
		case AlertTypeFailedQueueHigh:
			threshold := rule.Condition.FailedQueueThreshold
			if threshold > 0 && failedQueueDepth >= threshold && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("Failed issue queue depth %d exceeds threshold %d",
						failedQueueDepth, threshold))
				e.fireAlert(ctx, rule, alert)
			}

		case AlertTypeCircuitBreakerTrip:
			if cbTrips > 0 && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("Autopilot circuit breaker tripped (%d trips)", cbTrips))
				e.fireAlert(ctx, rule, alert)
			}

		case AlertTypeAPIErrorRateHigh:
			threshold := rule.Condition.APIErrorRatePerMin
			if threshold > 0 && apiErrorRate >= threshold && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("API error rate %.1f/min exceeds threshold %.1f/min",
						apiErrorRate, threshold))
				e.fireAlert(ctx, rule, alert)
			}

		case AlertTypePRStuckWaitingCI:
			timeout := rule.Condition.PRStuckTimeout
			if timeout > 0 && prStuckCount > 0 && prMaxWaitMin >= timeout.Minutes() && e.shouldFire(rule) {
				alert := e.createAlert(rule, event,
					fmt.Sprintf("%d PR(s) stuck in waiting_ci for %.0f+ minutes",
						prStuckCount, prMaxWaitMin))
				e.fireAlert(ctx, rule, alert)
			}

		// GH-849: Deadlock detection
		case AlertTypeDeadlock:
			timeout := rule.Condition.DeadlockTimeout
			if timeout == 0 {
				timeout = 1 * time.Hour // Default to 1 hour
			}

			noProgressMin := 0.0
			if v, ok := event.Metadata["no_progress_minutes"]; ok {
				_, _ = fmt.Sscanf(v, "%f", &noProgressMin)
			}

			deadlockAlertSent := false
			if v, ok := event.Metadata["deadlock_alert_sent"]; ok {
				deadlockAlertSent = v == "true"
			}

			// Only fire if:
			// 1. No progress for longer than timeout
			// 2. We haven't already sent an alert for this stall
			// 3. Rule cooldown allows firing
			if noProgressMin >= timeout.Minutes() && !deadlockAlertSent && e.shouldFire(rule) {
				lastState := event.Metadata["last_known_state"]
				lastPR := event.Metadata["last_known_pr"]

				message := fmt.Sprintf("No state transitions in %.0f minutes.", noProgressMin)
				if lastState != "" && lastPR != "0" {
					message = fmt.Sprintf("No state transitions in %.0f minutes. Last: %s for PR #%s",
						noProgressMin, lastState, lastPR)
				}

				alert := e.createAlert(rule, event, message)
				e.fireAlert(ctx, rule, alert)
			}
		}
	}
}

// handleEscalation processes escalation events (GH-885).
// These are critical alerts that should route to PagerDuty.
func (e *Engine) handleEscalation(ctx context.Context, event Event) {
	for _, rule := range e.config.Rules {
		if !rule.Enabled || rule.Type != AlertTypeEscalation {
			continue
		}

		if !e.shouldFire(rule) {
			continue
		}

		tripsInHour := event.Metadata["trips_in_hour"]
		threshold := event.Metadata["escalation_threshold"]
		lastPR := event.Metadata["last_pr"]
		lastReason := event.Metadata["last_reason"]

		message := fmt.Sprintf(
			"Circuit breaker escalation: %s trips in 1 hour (threshold: %s). Last: PR #%s - %s",
			tripsInHour, threshold, lastPR, lastReason,
		)

		alert := e.createAlert(rule, event, message)
		// Force critical severity for escalations
		alert.Severity = SeverityCritical
		e.fireAlert(ctx, rule, alert)
	}
}
