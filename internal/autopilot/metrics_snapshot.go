package autopilot

import (
	"time"
)

// --- Read accessors ---

// Snapshot returns a point-in-time copy of all metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := MetricsSnapshot{
		IssuesProcessed:            copyStringIntMap(m.IssuesProcessed),
		PRsMerged:                  m.PRsMerged,
		PRsFailed:                  m.PRsFailed,
		PRsConflicting:             m.PRsConflicting,
		CircuitBreakerTrips:        m.CircuitBreakerTrips,
		APIErrors:                  copyStringIntMap(m.APIErrors),
		LabelCleanups:              copyStringIntMap(m.LabelCleanups),
		ApprovalPersistMisses:      copyStringIntMap(m.ApprovalPersistMisses),
		TokensConsumed:             copyTokenKeyMap(m.TokensConsumed),
		ExecutionCostUSD:           copyStringFloatMap(m.ExecutionCostUSD),
		ExecutionsByResult:         copyExecKeyMap(m.ExecutionsByResult),
		PollerSkipped:              copyPollerSkipKeyMap(m.PollerSkipped),
		PollerDispatched:           copyStringIntMap(m.PollerDispatched),
		PollerDeferredScopeOverlap: copyStringIntMap(m.PollerDeferredScopeOverlap),
		OrphanPRsRegistered:        copyStringIntMap(m.OrphanPRsRegistered),
		ActivePRsByStage:           copyStageIntMap(m.ActivePRsByStage),
		QueueDepth:                 m.QueueDepth,
		FailedQueueDepth:           m.FailedQueueDepth,
		TotalActivePRs:             sumStageMap(m.ActivePRsByStage),
		AvgPRTimeToMerge:           avgDuration(m.PRTimeToMerge),
		AvgCIWaitDuration:          avgDuration(m.CIWaitDurations),
		AvgExecutionDuration:       avgDuration(m.ExecutionDurations),
		APIErrorRate:               m.apiErrorRate(),
		SnapshotAt:                 time.Now(),
	}

	// Calculate success rate
	total := int64(0)
	for _, v := range m.IssuesProcessed {
		total += v
	}
	if total > 0 {
		snap.SuccessRate = float64(m.IssuesProcessed["success"]) / float64(total)
	}

	return snap
}

// apiErrorRate returns errors per minute in the last 5 minutes.
// Must be called with mu held (at least RLock).
func (m *Metrics) apiErrorRate() float64 {
	cutoff := time.Now().Add(-5 * time.Minute)
	count := 0
	for _, t := range m.apiErrorTimes {
		if t.After(cutoff) {
			count++
		}
	}
	return float64(count) / 5.0 // per minute
}

// MetricsSnapshot is a read-only copy of metrics at a point in time.
type MetricsSnapshot struct {
	// Counters
	IssuesProcessed       map[string]int64
	PRsMerged             int64
	PRsFailed             int64
	PRsConflicting        int64
	CircuitBreakerTrips   int64
	APIErrors             map[string]int64
	LabelCleanups         map[string]int64
	ApprovalPersistMisses map[string]int64
	TokensConsumed        map[tokenKey]int64
	ExecutionCostUSD      map[string]float64
	ExecutionsByResult    map[execKey]int64

	// Poller dispatch/skip counters (TASK-293)
	PollerSkipped              map[pollerSkipKey]int64
	PollerDispatched           map[string]int64
	PollerDeferredScopeOverlap map[string]int64

	// Orphan PR registration (TASK-302)
	OrphanPRsRegistered map[string]int64 // trigger → count

	// Gauges
	ActivePRsByStage map[PRStage]int
	TotalActivePRs   int
	QueueDepth       int
	FailedQueueDepth int

	// Computed summaries
	SuccessRate          float64
	AvgPRTimeToMerge     time.Duration
	AvgCIWaitDuration    time.Duration
	AvgExecutionDuration time.Duration
	APIErrorRate         float64 // errors per minute (5m window)

	SnapshotAt time.Time
}

// TotalIssuesProcessed returns the sum of all issue results.
func (s MetricsSnapshot) TotalIssuesProcessed() int64 {
	var total int64
	for _, v := range s.IssuesProcessed {
		total += v
	}
	return total
}

// HistogramData contains raw duration samples for histogram computation.
type HistogramData struct {
	PRTimeToMerge      []time.Duration
	CIWaitDurations    []time.Duration
	ExecutionDurations []time.Duration
}

// HistogramSnapshot returns a copy of raw histogram samples.
func (m *Metrics) HistogramSnapshot() HistogramData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return HistogramData{
		PRTimeToMerge:      copyDurations(m.PRTimeToMerge),
		CIWaitDurations:    copyDurations(m.CIWaitDurations),
		ExecutionDurations: copyDurations(m.ExecutionDurations),
	}
}

func copyDurations(src []time.Duration) []time.Duration {
	if src == nil {
		return nil
	}
	dst := make([]time.Duration, len(src))
	copy(dst, src)
	return dst
}

// --- helpers ---

func copyStringIntMap(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStageIntMap(src map[PRStage]int) map[PRStage]int {
	dst := make(map[PRStage]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func sumStageMap(m map[PRStage]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func avgDuration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range samples {
		sum += d
	}
	return sum / time.Duration(len(samples))
}

func copyTokenKeyMap(src map[tokenKey]int64) map[tokenKey]int64 {
	dst := make(map[tokenKey]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStringFloatMap(src map[string]float64) map[string]float64 {
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyExecKeyMap(src map[execKey]int64) map[execKey]int64 {
	dst := make(map[execKey]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyPollerSkipKeyMap(src map[pollerSkipKey]int64) map[pollerSkipKey]int64 {
	dst := make(map[pollerSkipKey]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
