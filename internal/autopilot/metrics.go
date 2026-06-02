package autopilot

import (
	"sync"
	"time"
)

// tokenKey identifies a token bucket by model and direction (input/output/cache_read/etc).
type tokenKey struct {
	Model     string
	Direction string
}

// execKey identifies an execution bucket by model and result (success/failed/etc).
type execKey struct {
	Model  string
	Result string
}

// pollerSkipKey identifies a poller skip event by repo and reason.
type pollerSkipKey struct {
	Repo   string
	Reason string
}

// Metrics collects autopilot operational metrics.
// All methods are goroutine-safe.
type Metrics struct {
	mu sync.RWMutex

	// Counters
	IssuesProcessed       map[string]int64 // result → count (success, failed, rate_limited)
	PRsMerged             int64
	PRsFailed             int64
	PRsConflicting        int64
	CircuitBreakerTrips   int64
	APIErrors             map[string]int64 // endpoint → count
	LabelCleanups         map[string]int64 // label → count
	ApprovalPersistMisses map[string]int64 // kind → count (request_id, decision)
	// TokensConsumed, ExecutionCostUSD, and ExecutionsByResult are persisted per
	// snapshot to SQLite (GH-2856) so historical data survives across runs.
	// However, on daemon restart the in-memory counters reset to zero and
	// re-accumulate from new executions — they are NOT restored from the latest
	// snapshot yet. Prometheus rate() queries tolerate this via reset detection.
	// TODO(GH-2836): call Metrics.RestoreFromRow on startup to resume from last snapshot.
	TokensConsumed     map[tokenKey]int64 // {model,direction} → token count
	ExecutionCostUSD   map[string]float64 // model → cumulative USD cost
	ExecutionsByResult map[execKey]int64  // {model,result} → execution count

	// Poller dispatch/skip counters (GH-3064, TASK-293)
	PollerSkipped              map[pollerSkipKey]int64 // {repo,reason} → skip count
	PollerDispatched           map[string]int64        // repo → dispatch count
	PollerDeferredScopeOverlap map[string]int64        // repo → deferred-scope-overlap count

	// Orphan PR registration counters (GH-3113, TASK-302)
	// trigger is "reconciler" or "startup_scan".
	// Sustained spikes indicate OnPRCreated is missing fires.
	OrphanPRsRegistered map[string]int64 // trigger → count

	// Gauges (point-in-time values)
	ActivePRsByStage map[PRStage]int
	QueueDepth       int // issues with `pilot` label, no `pilot-in-progress`
	FailedQueueDepth int // issues with `pilot-failed`

	// Histograms (stored as recent samples for summary stats)
	PRTimeToMerge      []time.Duration
	CIWaitDurations    []time.Duration
	ExecutionDurations []time.Duration

	// Timestamps for rate calculation
	apiErrorTimes []time.Time

	// Maximum samples to keep for histograms
	maxSamples int
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		IssuesProcessed:            make(map[string]int64),
		APIErrors:                  make(map[string]int64),
		LabelCleanups:              make(map[string]int64),
		ApprovalPersistMisses:      make(map[string]int64),
		TokensConsumed:             make(map[tokenKey]int64),
		ExecutionCostUSD:           make(map[string]float64),
		ExecutionsByResult:         make(map[execKey]int64),
		PollerSkipped:              make(map[pollerSkipKey]int64),
		PollerDispatched:           make(map[string]int64),
		PollerDeferredScopeOverlap: make(map[string]int64),
		OrphanPRsRegistered:        make(map[string]int64),
		ActivePRsByStage:           make(map[PRStage]int),
		PRTimeToMerge:              make([]time.Duration, 0, 100),
		CIWaitDurations:            make([]time.Duration, 0, 100),
		ExecutionDurations:         make([]time.Duration, 0, 100),
		apiErrorTimes:              make([]time.Time, 0, 100),
		maxSamples:                 1000,
	}
}
