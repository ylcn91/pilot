package autopilot

import (
	"time"
)

// --- Counter increments ---

// RecordIssueProcessed increments the processed issue counter by result.
func (m *Metrics) RecordIssueProcessed(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IssuesProcessed[result]++
}

// RecordPollerSkipped increments the poller skip counter for a given repo and reason.
func (m *Metrics) RecordPollerSkipped(repo, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PollerSkipped[pollerSkipKey{Repo: repo, Reason: reason}]++
}

// RecordPollerDispatched increments the dispatch counter for a repo.
func (m *Metrics) RecordPollerDispatched(repo string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PollerDispatched[repo]++
}

// RecordPollerDeferredScopeOverlap increments the scope-overlap deferral counter for a repo.
func (m *Metrics) RecordPollerDeferredScopeOverlap(repo string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PollerDeferredScopeOverlap[repo]++
}

// RecordOrphanPRRegistered increments the orphan-PR registration counter.
// trigger is "reconciler" (periodic loop) or "startup_scan" (ScanExistingPRs).
func (m *Metrics) RecordOrphanPRRegistered(trigger string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.OrphanPRsRegistered[trigger]++
}

// RecordPRMerged increments the merged PR counter.
func (m *Metrics) RecordPRMerged() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PRsMerged++
}

// RecordPRFailed increments the failed PR counter.
func (m *Metrics) RecordPRFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PRsFailed++
}

// RecordPRConflicting increments the conflicting PR counter.
func (m *Metrics) RecordPRConflicting() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PRsConflicting++
}

// RecordCircuitBreakerTrip increments the circuit breaker trip counter.
func (m *Metrics) RecordCircuitBreakerTrip() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CircuitBreakerTrips++
}

// RecordAPIError increments the API error counter for a given endpoint.
func (m *Metrics) RecordAPIError(endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.APIErrors[endpoint]++
	m.apiErrorTimes = append(m.apiErrorTimes, time.Now())
	// Trim old entries (keep last maxSamples)
	if len(m.apiErrorTimes) > m.maxSamples {
		m.apiErrorTimes = m.apiErrorTimes[len(m.apiErrorTimes)-m.maxSamples:]
	}
}

// RecordLabelCleanup increments the label cleanup counter.
func (m *Metrics) RecordLabelCleanup(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LabelCleanups[label]++
}

// RecordApprovalPersistMiss increments the counter for zero-row approval UPDATE misses.
// kind is "request_id" or "decision".
func (m *Metrics) RecordApprovalPersistMiss(kind string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApprovalPersistMisses[kind]++
}

// RecordTokens adds n tokens to the {model, direction} bucket.
func (m *Metrics) RecordTokens(model, direction string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TokensConsumed[tokenKey{Model: model, Direction: direction}] += n
}

// RecordCost adds costUSD to the per-model cumulative cost.
func (m *Metrics) RecordCost(model string, costUSD float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecutionCostUSD[model] += costUSD
}

// RecordExecution increments the {model, result} execution counter.
func (m *Metrics) RecordExecution(model, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecutionsByResult[execKey{Model: model, Result: result}]++
}

// --- Gauge updates ---

// UpdateActivePRs recalculates active PR counts by stage from a snapshot.
func (m *Metrics) UpdateActivePRs(prs []*PRState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset all stages
	m.ActivePRsByStage = make(map[PRStage]int)
	for _, pr := range prs {
		m.ActivePRsByStage[pr.Stage]++
	}
}

// SetQueueDepth updates the queue depth gauge.
func (m *Metrics) SetQueueDepth(depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.QueueDepth = depth
}

// SetFailedQueueDepth updates the failed queue depth gauge.
func (m *Metrics) SetFailedQueueDepth(depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedQueueDepth = depth
}

// --- Histogram recording ---

// RecordPRTimeToMerge records the duration from PR creation to merge.
func (m *Metrics) RecordPRTimeToMerge(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PRTimeToMerge = append(m.PRTimeToMerge, d)
	if len(m.PRTimeToMerge) > m.maxSamples {
		m.PRTimeToMerge = m.PRTimeToMerge[len(m.PRTimeToMerge)-m.maxSamples:]
	}
}

// RecordCIWaitDuration records how long a PR waited for CI.
func (m *Metrics) RecordCIWaitDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CIWaitDurations = append(m.CIWaitDurations, d)
	if len(m.CIWaitDurations) > m.maxSamples {
		m.CIWaitDurations = m.CIWaitDurations[len(m.CIWaitDurations)-m.maxSamples:]
	}
}

// RecordExecutionDuration records task execution time.
func (m *Metrics) RecordExecutionDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecutionDurations = append(m.ExecutionDurations, d)
	if len(m.ExecutionDurations) > m.maxSamples {
		m.ExecutionDurations = m.ExecutionDurations[len(m.ExecutionDurations)-m.maxSamples:]
	}
}
