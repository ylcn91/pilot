package architect

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// SchedulerConfig is the cron-scheduling slice of the Architect config the
// scheduler needs. It is deliberately decoupled from config.ArchitectConfig so
// internal/architect stays a leaf package (no import of internal/config): the
// daemon wiring projects the relevant fields into this struct at construction.
type SchedulerConfig struct {
	// Enabled gates the whole scheduler. False => Start is a logged no-op.
	Enabled bool

	// Schedule is a standard 5-field cron expression. Empty => Start is a
	// logged no-op even when Enabled (on-demand-only operation).
	Schedule string

	// Timezone names the IANA zone the Schedule is evaluated in. Empty or
	// unparseable falls back to UTC.
	Timezone string
}

// Scheduler periodically runs the radar lens and refreshes a FindingsStore so
// the dashboard sink (web/TUI/desktop) always reflects a recent architectural
// drift + tech-debt sweep. It mirrors internal/briefs.Scheduler's lifecycle
// (Start/Stop/RunNow/maybeCatchUp) but drives the offline RunRadar pipeline and
// writes findings into the in-memory store rather than delivering messages.
//
// The store is the seam to the gateway: the daemon injects the same store both
// here (as the scan sink) and into gateway.Server via SetArchitectProvider, so
// each scheduled scan's result is immediately visible to dashboard clients.
type Scheduler struct {
	radar   RadarConfig
	sched   SchedulerConfig
	store   *FindingsStore
	cron    *cron.Cron
	mu      sync.Mutex
	running bool
	entryID cron.EntryID
	logger  *slog.Logger
	// scanned reports whether at least one scan has completed since
	// construction; it backs maybeCatchUp's "never scanned" decision so a
	// freshly-started daemon populates the dashboard immediately instead of
	// waiting for the first cron tick.
	scanned bool
}

// NewScheduler builds an Architect radar scheduler. The store is the sink each
// scan writes to and must be non-nil for the scheduler to do anything useful;
// the daemon constructs it once and shares it with the gateway. A nil logger
// falls back to slog.Default. The timezone is resolved here (falling back to
// UTC on error) so cron entries fire in the operator's intended zone.
func NewScheduler(radar RadarConfig, sched SchedulerConfig, store *FindingsStore, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	loc, err := time.LoadLocation(sched.Timezone)
	if err != nil || loc == nil {
		if sched.Timezone != "" {
			logger.Warn("architect scheduler: invalid timezone, using UTC",
				"timezone", sched.Timezone, "error", err)
		}
		loc = time.UTC
	}

	return &Scheduler{
		radar:  radar,
		sched:  sched,
		store:  store,
		cron:   cron.New(cron.WithLocation(loc)),
		logger: logger,
	}
}

// Start registers the cron job and begins ticking. It is a no-op (returning
// nil) when the scheduler is already running, when the config is disabled, or
// when no schedule is set — the last case is the legitimate "on-demand only"
// mode where the daemon still exposes the store but never auto-scans.
//
// After a successful start it runs maybeCatchUp, which fires one immediate scan
// if the store has never been populated, so the dashboard shows findings from
// boot rather than after the first cron interval elapses.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	if !s.sched.Enabled {
		s.logger.Info("architect radar scheduler disabled")
		return nil
	}

	if s.sched.Schedule == "" {
		s.logger.Info("architect radar scheduler has no schedule, on-demand only")
		return nil
	}

	entryID, err := s.cron.AddFunc(s.sched.Schedule, func() {
		s.runScan(ctx)
	})
	if err != nil {
		return err
	}

	s.entryID = entryID
	s.cron.Start()
	s.running = true

	nextRun := s.cron.Entry(s.entryID).Next
	s.logger.Info("architect radar scheduler started",
		"schedule", s.sched.Schedule,
		"timezone", s.sched.Timezone,
		"project_path", s.radar.ProjectPath,
		"next_run", nextRun,
	)

	s.maybeCatchUp(ctx)
	return nil
}

// Stop halts the cron scheduler and blocks until any in-flight scan finishes.
// It is safe to call when not running and safe to call repeatedly.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	ctx := s.cron.Stop()
	<-ctx.Done()
	s.running = false
	s.logger.Info("architect radar scheduler stopped")
}

// IsRunning reports whether the cron loop is active.
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// NextRun returns the next scheduled scan time, or the zero time when the
// scheduler is not running.
func (s *Scheduler) NextRun() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return time.Time{}
	}
	return s.cron.Entry(s.entryID).Next
}

// LastRun returns the previous scheduled scan time, or the zero time when the
// scheduler is not running.
func (s *Scheduler) LastRun() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return time.Time{}
	}
	return s.cron.Entry(s.entryID).Prev
}

// RunNow triggers an immediate radar scan and store update, bypassing the cron
// schedule. It returns RunRadar's error (misconfiguration or context
// cancellation) so on-demand callers can surface a failure. The cron-driven
// path swallows the error into a log line instead.
func (s *Scheduler) RunNow(ctx context.Context) error {
	return s.scan(ctx)
}

// runScan is the cron callback: it runs a scan and logs any failure rather than
// propagating it (a cron func has nowhere to return an error to).
func (s *Scheduler) runScan(ctx context.Context) {
	if err := s.scan(ctx); err != nil {
		s.logger.Error("architect radar scan failed", "error", err)
	}
}

// scan runs RunRadar and records that a scan has completed (used by
// maybeCatchUp). The scanned flag is set only on success so a failed boot scan
// still triggers catch-up logic on a later opportunity.
func (s *Scheduler) scan(ctx context.Context) error {
	s.logger.Info("architect radar scan starting", "project_path", s.radar.ProjectPath)

	if err := RunRadar(ctx, s.radar, s.store); err != nil {
		return err
	}

	s.mu.Lock()
	s.scanned = true
	s.mu.Unlock()

	s.logger.Info("architect radar scan complete", "findings", s.store.Len())
	return nil
}

// maybeCatchUp fires one immediate scan when the store has never been populated
// by this scheduler, so a daemon that starts between cron ticks still shows a
// current radar instead of an empty panel. Once a scan has run, subsequent
// Starts (e.g. after a transient Stop) do not re-trigger a catch-up.
//
// It is called with s.mu held (from Start), so it does not lock; the scan it
// dispatches runs in a new goroutine to avoid blocking Start on a full project
// sweep.
func (s *Scheduler) maybeCatchUp(ctx context.Context) {
	if s.scanned || s.store.Len() > 0 {
		s.logger.Info("architect radar catch-up skipped: store already populated")
		return
	}

	s.logger.Info("architect radar catch-up: no prior scan, scanning now")
	go s.runScan(ctx)
}
