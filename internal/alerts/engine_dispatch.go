package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// shouldFire checks if a rule should fire based on cooldown
func (e *Engine) shouldFire(rule AlertRule) bool {
	if rule.Cooldown == 0 {
		return true
	}

	e.mu.RLock()
	lastFired, exists := e.lastAlertTimes[rule.Name]
	e.mu.RUnlock()

	if !exists {
		return true
	}

	return time.Since(lastFired) >= rule.Cooldown
}

// createAlert creates an alert from a rule and event
func (e *Engine) createAlert(rule AlertRule, event Event, message string) *Alert {
	source := ""
	if event.TaskID != "" {
		source = fmt.Sprintf("task:%s", event.TaskID)
	}

	return &Alert{
		ID:          uuid.New().String(),
		Type:        rule.Type,
		Severity:    rule.Severity,
		Title:       rule.Description,
		Message:     message,
		Source:      source,
		ProjectPath: event.Project,
		Metadata:    event.Metadata,
		CreatedAt:   time.Now(),
	}
}

// createEscalationAlert creates an escalation alert for PagerDuty incident creation (GH-848)
func (e *Engine) createEscalationAlert(rule AlertRule, event Event, source string, retryCount int) *Alert {
	metadata := make(map[string]string)
	for k, v := range event.Metadata {
		metadata[k] = v
	}
	metadata["retry_count"] = fmt.Sprintf("%d", retryCount)
	metadata["escalation_source"] = source

	return &Alert{
		ID:          uuid.New().String(),
		Type:        AlertTypeEscalation,
		Severity:    SeverityCritical,
		Title:       "Escalation: Repeated failures require human intervention",
		Message:     fmt.Sprintf("Source %s has failed %d consecutive times. Last error: %s", source, retryCount, event.Error),
		Source:      source,
		ProjectPath: event.Project,
		Metadata:    metadata,
		CreatedAt:   time.Now(),
	}
}

// fireAlert sends an alert through configured channels. Delivery is handed to a
// bounded background goroutine so a slow/hung channel cannot block the event
// loop (E1); identical alerts are suppressed within a TTL window (E5).
func (e *Engine) fireAlert(ctx context.Context, rule AlertRule, alert *Alert) {
	now := time.Now()

	e.mu.Lock()
	// E5: suppress identical alerts ({rule|source|message}) within the TTL window.
	if e.config.Defaults.SuppressDuplicates {
		key := dedupeKey(rule.Name, alert.Source, alert.Message)
		if last, ok := e.recentAlerts[key]; ok && now.Sub(last) < duplicateSuppressTTL {
			e.mu.Unlock()
			e.metrics.RecordDropped()
			e.logger.Debug("duplicate alert suppressed",
				"rule", rule.Name,
				"source", alert.Source,
				"alert_id", alert.ID,
			)
			return
		}
		e.recentAlerts[key] = now
		e.pruneRecentAlertsLocked(now)
	}
	e.lastAlertTimes[rule.Name] = now
	e.mu.Unlock()

	e.metrics.RecordFired(rule.Name, string(alert.Severity))

	if e.dispatcher == nil {
		e.logger.Warn("no dispatcher configured, alert not sent",
			"rule", rule.Name,
			"alert_id", alert.ID,
		)
		return
	}

	// Determine which channels to send to
	channels := rule.Channels
	if len(channels) == 0 {
		// Send to all channels that accept this severity
		for _, ch := range e.config.Channels {
			if ch.Enabled && e.channelAcceptsSeverity(ch, alert.Severity) {
				channels = append(channels, ch.Name)
			}
		}
	}

	// E1: once running, hand delivery to the background worker so a slow/hung
	// channel can never block the event loop. Before Start() (direct callers /
	// tests) deliver inline so the path stays synchronous.
	if !e.started.Load() {
		e.dispatchAndRecord(ctx, rule, alert, channels)
		return
	}
	e.dispatchWG.Add(1)
	select {
	case e.dispatchCh <- dispatchJob{rule: rule, alert: alert, channels: channels}:
	default:
		e.dispatchWG.Done()
		e.metrics.RecordDropped()
		e.logger.Warn("alert dispatch backlog full, dropping delivery",
			"rule", rule.Name,
			"alert_id", alert.ID,
			"severity", alert.Severity,
		)
	}
}

// dispatchAndRecord delivers an alert to its channels and records delivery
// history. Runs in its own goroutine, bounded by dispatchSem (E1).
func (e *Engine) dispatchAndRecord(ctx context.Context, rule AlertRule, alert *Alert, channels []string) {
	results := e.dispatcher.Dispatch(ctx, alert, channels)

	// Track delivery history
	deliveredTo := make([]string, 0)
	for _, r := range results {
		if r.Success {
			deliveredTo = append(deliveredTo, r.ChannelName)
		} else {
			e.logger.Error("failed to deliver alert",
				"channel", r.ChannelName,
				"error", r.Error,
			)
		}
	}

	e.mu.Lock()
	e.alertHistory = append(e.alertHistory, AlertHistory{
		AlertID:     alert.ID,
		RuleName:    rule.Name,
		Source:      alert.Source,
		FiredAt:     alert.CreatedAt,
		DeliveredTo: deliveredTo,
	})
	// Keep only last 1000 alerts in history
	if len(e.alertHistory) > 1000 {
		e.alertHistory = e.alertHistory[len(e.alertHistory)-1000:]
	}
	e.mu.Unlock()

	e.logger.Info("alert fired",
		"rule", rule.Name,
		"alert_id", alert.ID,
		"severity", alert.Severity,
		"delivered_to", deliveredTo,
	)
}

// dedupeKey builds the suppression key for an alert (E5).
func dedupeKey(rule, source, message string) string {
	return rule + "|" + source + "|" + message
}

// pruneRecentAlertsLocked removes dedupe entries older than the TTL.
// The caller must hold e.mu.
func (e *Engine) pruneRecentAlertsLocked(now time.Time) {
	for k, t := range e.recentAlerts {
		if now.Sub(t) >= duplicateSuppressTTL {
			delete(e.recentAlerts, k)
		}
	}
}

func (e *Engine) channelAcceptsSeverity(ch ChannelConfig, severity Severity) bool {
	if len(ch.Severities) == 0 {
		return true // Accept all severities by default
	}
	for _, s := range ch.Severities {
		if s == severity {
			return true
		}
	}
	return false
}
