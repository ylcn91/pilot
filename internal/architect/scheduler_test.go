package architect

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// fixtureRadar returns a RadarConfig pointed at a temp project containing one
// oversized Go file, which the LOC collector flags deterministically and
// offline — so a scan against it always yields at least one finding.
func fixtureRadar(t *testing.T) RadarConfig {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "big.go"), oversizedGoFile())
	return RadarConfig{ProjectPath: root}
}

// emptyRadar returns a RadarConfig pointed at an empty temp project, which
// yields zero findings from the deterministic roster.
func emptyRadar(t *testing.T) RadarConfig {
	t.Helper()
	return RadarConfig{ProjectPath: t.TempDir()}
}

func enabledSched(schedule string) SchedulerConfig {
	return SchedulerConfig{Enabled: true, Schedule: schedule, Timezone: "UTC"}
}

func TestScheduler_RunNowPopulatesStore(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), enabledSched("0 9 * * *"), store, nil)

	if err := s.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if store.Len() == 0 {
		t.Fatal("RunNow must populate the store with at least one finding")
	}

	var sawBig bool
	for _, f := range store.Findings() {
		if !f.Risk.IsValid() || f.Risk == "" {
			t.Errorf("finding has invalid risk %q", f.Risk)
		}
		for _, file := range f.Files {
			if filepath.Base(file) == "big.go" {
				sawBig = true
			}
		}
	}
	if !sawBig {
		t.Fatalf("expected a finding referencing big.go, got %+v", store.Findings())
	}
}

func TestScheduler_RunNowError(t *testing.T) {
	store := NewFindingsStore()
	// Empty ProjectPath makes RunRadar return a misconfiguration error, which
	// RunNow surfaces (unlike the cron path, which only logs).
	s := NewScheduler(RadarConfig{}, enabledSched("0 9 * * *"), store, nil)

	if err := s.RunNow(context.Background()); err == nil {
		t.Fatal("RunNow with empty ProjectPath must return an error")
	}
	if store.Len() != 0 {
		t.Fatalf("failed scan must not populate the store, got %d", store.Len())
	}
}

func TestScheduler_StartDisabled(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), SchedulerConfig{Enabled: false, Schedule: "0 9 * * *"}, store, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if s.IsRunning() {
		t.Fatal("disabled scheduler must not be running")
	}
	if store.Len() != 0 {
		t.Fatal("disabled scheduler must not scan")
	}
	if !s.NextRun().IsZero() {
		t.Fatal("disabled scheduler NextRun must be zero")
	}
}

func TestScheduler_StartNoSchedule(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), SchedulerConfig{Enabled: true, Schedule: ""}, store, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if s.IsRunning() {
		t.Fatal("scheduleless scheduler must not be running")
	}
	if store.Len() != 0 {
		t.Fatal("scheduleless scheduler must not scan on Start")
	}
}

func TestScheduler_StartInvalidCron(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), enabledSched("not a cron"), store, nil)

	if err := s.Start(context.Background()); err == nil {
		t.Fatal("invalid cron must return an error from Start")
	}
	if s.IsRunning() {
		t.Fatal("scheduler must not be running after a failed Start")
	}
}

func TestScheduler_StartEnabledRunsAndCatchesUp(t *testing.T) {
	store := NewFindingsStore()
	// "0 9 * * *" fires at most once a day, so the populated store proves the
	// boot-time catch-up scan (not a cron tick) ran.
	s := NewScheduler(fixtureRadar(t), enabledSched("0 9 * * *"), store, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	if !s.IsRunning() {
		t.Fatal("enabled scheduler with a schedule must be running")
	}
	if s.NextRun().IsZero() {
		t.Fatal("running scheduler must report a future NextRun")
	}

	// The catch-up scan runs in a goroutine; wait for it to land.
	waitForStore(t, store, 1)
}

func TestScheduler_CatchUpSkippedWhenStorePopulated(t *testing.T) {
	store := NewFindingsStore()
	// Pre-populate the store so maybeCatchUp finds it non-empty and skips.
	store.Set([]pilotapi.Finding{{Title: "preexisting", Risk: pilotapi.RiskLow}})

	s := NewScheduler(emptyRadar(t), enabledSched("0 9 * * *"), store, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Give any (incorrectly-dispatched) catch-up goroutine time to clobber the
	// store; the pre-seeded finding must remain because catch-up was skipped.
	time.Sleep(150 * time.Millisecond)
	got := store.Findings()
	if len(got) != 1 || got[0].Title != "preexisting" {
		t.Fatalf("catch-up must be skipped when store is populated, got %+v", got)
	}
}

func TestScheduler_StopWhenNotRunning(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), enabledSched("0 9 * * *"), store, nil)
	// Stop without Start must be a clean no-op.
	s.Stop()
	if s.IsRunning() {
		t.Fatal("Stop before Start must leave scheduler stopped")
	}
}

func TestScheduler_DoubleStartIsNoop(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), enabledSched("0 9 * * *"), store, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer s.Stop()
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("second Start must be a no-op, got %v", err)
	}
	if !s.IsRunning() {
		t.Fatal("scheduler must remain running after double Start")
	}
}

func TestScheduler_DoubleStopIsClean(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), enabledSched("0 9 * * *"), store, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
	s.Stop() // must not panic
	if s.IsRunning() {
		t.Fatal("scheduler must be stopped after double Stop")
	}
	if !s.NextRun().IsZero() {
		t.Fatal("stopped scheduler NextRun must be zero")
	}
	if !s.LastRun().IsZero() {
		t.Fatal("stopped scheduler LastRun must be zero")
	}
}

func TestScheduler_RestartDoesNotRecatchUp(t *testing.T) {
	store := NewFindingsStore()
	s := NewScheduler(fixtureRadar(t), enabledSched("0 9 * * *"), store, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	waitForStore(t, store, 1)
	s.Stop()

	// Clear the store, then restart: because a scan already completed
	// (s.scanned == true), maybeCatchUp must NOT re-scan even though the store
	// is now empty.
	store.Set(nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer s.Stop()

	time.Sleep(150 * time.Millisecond)
	if store.Len() != 0 {
		t.Fatalf("restart must not re-trigger catch-up after a prior scan, got %d findings", store.Len())
	}
}

func TestScheduler_InvalidTimezoneFallsBackToUTC(t *testing.T) {
	store := NewFindingsStore()
	// A bogus timezone must not crash construction; it falls back to UTC and
	// the scheduler still starts.
	s := NewScheduler(fixtureRadar(t), SchedulerConfig{Enabled: true, Schedule: "0 9 * * *", Timezone: "Mars/Phobos"}, store, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start with bad timezone: %v", err)
	}
	defer s.Stop()
	if !s.IsRunning() {
		t.Fatal("scheduler must run despite an invalid timezone")
	}
}

func TestScheduler_CronTickRefreshesStore(t *testing.T) {
	store := NewFindingsStore()
	// "* * * * *" fires every minute. With a 65s budget the cron tick refreshes
	// the store on its own (independent of the boot catch-up): we clear the
	// store after catch-up, then wait for the tick to repopulate it.
	if testing.Short() {
		t.Skip("skipping minute-granularity cron test in -short mode")
	}
	s := NewScheduler(fixtureRadar(t), enabledSched("* * * * *"), store, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	waitForStore(t, store, 1) // boot catch-up
	store.Set(nil)

	deadline := time.Now().Add(70 * time.Second)
	for time.Now().Before(deadline) {
		if store.Len() > 0 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("cron tick did not refresh the store within the window")
}

// waitForStore polls until the store holds at least want findings or the
// 3-second deadline elapses (catch-up scans run in a goroutine).
func waitForStore(t *testing.T, store *FindingsStore, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if store.Len() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("store did not reach %d findings within deadline (have %d)", want, store.Len())
}
