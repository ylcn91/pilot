package alerts

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Engine is the core alerting engine that processes events and triggers alerts
type Engine struct {
	config     *AlertConfig
	dispatcher *Dispatcher
	logger     *slog.Logger

	// State tracking
	mu                  sync.RWMutex
	lastAlertTimes      map[string]time.Time     // rule name -> last fired time
	consecutiveFailures map[string]int           // project -> consecutive failure count
	taskLastProgress    map[string]progressState // task ID -> last progress state
	alertHistory        []AlertHistory
	retryTracker        map[string]int // source (issue/PR) -> consecutive failure count (GH-848)

	// Channels for events. priorityCh carries high-severity events
	// (escalation / OOM / budget / security) on a dedicated buffer so a flood of
	// ordinary task events filling eventCh cannot starve a critical alert (E1).
	eventCh    chan Event
	priorityCh chan Event
	done       chan struct{}

	// dispatchCh feeds a single background delivery worker. fireAlert enqueues
	// here instead of calling Dispatch inline, so a slow/hung channel can never
	// block the event loop (E1). Sequential delivery keeps ordering and adds no
	// new concurrency. dispatchWG tracks in-flight deliveries for WaitForDispatch.
	dispatchCh chan dispatchJob
	dispatchWG sync.WaitGroup
	// started is set once Start() launches the dispatch worker. Before that
	// (direct callers / tests) fireAlert delivers inline so behavior is synchronous.
	started atomic.Bool

	// recentAlerts deduplicates identical alerts ({rule|source|message}) within
	// duplicateSuppressTTL when config.Defaults.SuppressDuplicates is set (E5).
	recentAlerts map[string]time.Time

	// metrics accumulates fired/dropped counters; share the same instance with the
	// Dispatcher via WithAlertMetrics+WithDispatcherMetrics so delivery counts appear
	// in AlertSnapshot too.
	metrics *AlertMetrics
}

type progressState struct {
	Progress      int
	UpdatedAt     time.Time
	Phase         string
	LastAlertedAt time.Time // Per-task alert cooldown (GH-2204)
}

// Event represents an event that might trigger an alert
type Event struct {
	Type      EventType
	TaskID    string
	TaskTitle string
	Project   string
	Phase     string
	Progress  int
	Error     string
	Metadata  map[string]string
	Timestamp time.Time
	// test-only: set by flushForTest to drain the event queue
	testFlushResp chan struct{}
}

// EventType categorizes incoming events
type EventType string

const (
	EventTypeTaskStarted    EventType = "task_started"
	EventTypeTaskProgress   EventType = "task_progress"
	EventTypeTaskCompleted  EventType = "task_completed"
	EventTypeTaskFailed     EventType = "task_failed"
	EventTypeCostUpdate     EventType = "cost_update"
	EventTypeSecurityEvent  EventType = "security_event"
	EventTypeBudgetExceeded EventType = "budget_exceeded"
	EventTypeBudgetWarning  EventType = "budget_warning"

	// Autopilot health events (GH-728)
	EventTypeAutopilotMetrics EventType = "autopilot_metrics"

	// Escalation events (GH-885)
	EventTypeEscalation EventType = "escalation"

	// OOM-killed backend events (GH-2332). Routed through the task-failed
	// handler so consecutive-failure tracking keeps working, but kept as a
	// distinct type so rules and dashboards can single these out.
	EventTypeOOMKilled EventType = "oom_killed"
)

const (
	// dispatchBacklog bounds the delivery queue feeding the dispatch worker (E1).
	// When the worker is stuck on a hung channel and the backlog fills, further
	// deliveries are dropped with a counter rather than blocking the event loop.
	dispatchBacklog = 256
	// duplicateSuppressTTL is the window within which an identical alert
	// ({rule|source|message}) is suppressed when SuppressDuplicates is enabled (E5).
	duplicateSuppressTTL = 5 * time.Minute
)

// dispatchJob is a queued alert delivery handled by the dispatch worker (E1).
type dispatchJob struct {
	rule     AlertRule
	alert    *Alert
	channels []string
}

// EngineOption configures the Engine
type EngineOption func(*Engine)

// WithLogger sets the logger
func WithLogger(logger *slog.Logger) EngineOption {
	return func(e *Engine) {
		e.logger = logger
	}
}

// WithDispatcher sets the dispatcher
func WithDispatcher(d *Dispatcher) EngineOption {
	return func(e *Engine) {
		e.dispatcher = d
	}
}

// WithAlertMetrics injects a shared AlertMetrics instance.
// Pass the same instance to WithDispatcherMetrics so delivery counters from the
// Dispatcher appear in Engine.AlertSnapshot().
func WithAlertMetrics(m *AlertMetrics) EngineOption {
	return func(e *Engine) {
		e.metrics = m
	}
}

// NewEngine creates a new alerting engine
func NewEngine(config *AlertConfig, opts ...EngineOption) *Engine {
	e := &Engine{
		config:              config,
		logger:              slog.Default(),
		lastAlertTimes:      make(map[string]time.Time),
		consecutiveFailures: make(map[string]int),
		taskLastProgress:    make(map[string]progressState),
		alertHistory:        make([]AlertHistory, 0),
		retryTracker:        make(map[string]int),
		eventCh:             make(chan Event, 100),
		priorityCh:          make(chan Event, 100),
		done:                make(chan struct{}),
		dispatchCh:          make(chan dispatchJob, dispatchBacklog),
		recentAlerts:        make(map[string]time.Time),
		metrics:             NewAlertMetrics(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Start starts the alerting engine
func (e *Engine) Start(ctx context.Context) error {
	if !e.config.Enabled {
		e.logger.Info("alerting engine disabled")
		return nil
	}

	e.logger.Info("starting alerting engine",
		"rules", len(e.config.Rules),
		"channels", len(e.config.Channels),
	)

	// Start event processor
	go e.processEvents(ctx)

	// Start the delivery worker that drains dispatchCh (E1).
	go e.dispatchWorker(ctx)

	// Start stuck task checker
	go e.checkStuckTasks(ctx)

	e.started.Store(true)

	return nil
}

// dispatchWorker drains queued deliveries sequentially so channel I/O never
// blocks the event loop (E1).
func (e *Engine) dispatchWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case job := <-e.dispatchCh:
			e.dispatchAndRecord(ctx, job.rule, job.alert, job.channels)
			e.dispatchWG.Done()
		}
	}
}

// WaitForDispatch blocks until all enqueued alert deliveries have completed.
// Useful for graceful shutdown and for deterministic tests.
func (e *Engine) WaitForDispatch() {
	e.dispatchWG.Wait()
}

// flushForTest blocks until all events currently in the event queue have been
// processed and all in-flight dispatches complete. test-only: requires Start().
func (e *Engine) flushForTest() {
	resp := make(chan struct{})
	e.eventCh <- Event{testFlushResp: resp}
	<-resp
	e.WaitForDispatch()
}

// Stop stops the alerting engine
func (e *Engine) Stop() {
	close(e.done)
}

// ProcessEvent adds an event to the processing queue. High-severity events
// (escalation / OOM / budget / security) go on a dedicated priority queue so a
// flood of ordinary events that fills eventCh cannot drop them (E1).
func (e *Engine) ProcessEvent(event Event) {
	if !e.config.Enabled {
		return
	}

	if isHighPriorityEvent(event.Type) {
		select {
		case e.priorityCh <- event:
		default:
			// The priority queue is independent of eventCh, so this only happens
			// under a sustained critical-event storm. Log loudly and count it.
			e.logger.Error("CRITICAL alert event dropped — priority queue full",
				"type", event.Type,
				"task_id", event.TaskID,
			)
			e.metrics.RecordDropped()
		}
		return
	}

	select {
	case e.eventCh <- event:
	default:
		e.logger.Warn("alert event queue full, dropping event",
			"type", event.Type,
			"task_id", event.TaskID,
		)
		e.metrics.RecordDropped()
	}
}

// isHighPriorityEvent reports whether an event must not be lost under load.
func isHighPriorityEvent(t EventType) bool {
	switch t {
	case EventTypeEscalation, EventTypeOOMKilled, EventTypeBudgetExceeded, EventTypeSecurityEvent:
		return true
	default:
		return false
	}
}

// processEvents processes incoming events. The priority queue is drained ahead
// of the normal queue so critical alerts are handled first under load.
func (e *Engine) processEvents(ctx context.Context) {
	for {
		// Fast path: always prefer a pending priority event.
		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case event := <-e.priorityCh:
			e.handleEvent(ctx, event)
			continue
		default:
		}

		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case event := <-e.priorityCh:
			e.handleEvent(ctx, event)
		case event := <-e.eventCh:
			e.handleEvent(ctx, event)
		}
	}
}

// GetAlertHistory returns recent alert history
func (e *Engine) GetAlertHistory(limit int) []AlertHistory {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > len(e.alertHistory) {
		limit = len(e.alertHistory)
	}

	// Return most recent alerts first
	result := make([]AlertHistory, limit)
	for i := 0; i < limit; i++ {
		result[i] = e.alertHistory[len(e.alertHistory)-1-i]
	}
	return result
}

// GetConfig returns the current alert configuration
func (e *Engine) GetConfig() *AlertConfig {
	return e.config
}

// UpdateConfig updates the alert configuration
func (e *Engine) UpdateConfig(config *AlertConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

// AlertSnapshot returns a point-in-time copy of alert metrics including the current
// event queue depth. When the same AlertMetrics instance is shared with the Dispatcher
// via WithAlertMetrics + WithDispatcherMetrics, the snapshot also includes delivery counters.
func (e *Engine) AlertSnapshot() AlertMetricsSnapshot {
	snap := e.metrics.Snapshot()
	snap.QueueDepth = len(e.eventCh)
	return snap
}
