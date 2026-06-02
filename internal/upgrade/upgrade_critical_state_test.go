package upgrade

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// State helpers
// ---------------------------------------------------------------------------

func TestState_MarkFailed(t *testing.T) {
	s := &State{Status: StatusInstalling}
	s.MarkFailed(fmt.Errorf("disk full"))

	if s.Status != StatusFailed {
		t.Errorf("status = %q, want %q", s.Status, StatusFailed)
	}
	if s.Error != "disk full" {
		t.Errorf("error = %q, want %q", s.Error, "disk full")
	}
}

func TestState_MarkFailed_NilError(t *testing.T) {
	s := &State{Status: StatusInstalling}
	s.MarkFailed(nil)

	if s.Status != StatusFailed {
		t.Errorf("status = %q, want %q", s.Status, StatusFailed)
	}
	if s.Error != "" {
		t.Errorf("error = %q, want empty", s.Error)
	}
}

func TestState_MarkCompleted(t *testing.T) {
	s := &State{Status: StatusInstalling}
	s.MarkCompleted()

	if s.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", s.Status, StatusCompleted)
	}
	if s.UpgradeCompleted.IsZero() {
		t.Error("UpgradeCompleted should be set")
	}
}

func TestState_MarkRolledBack(t *testing.T) {
	s := &State{Status: StatusFailed}
	s.MarkRolledBack()

	if s.Status != StatusRolledBack {
		t.Errorf("status = %q, want %q", s.Status, StatusRolledBack)
	}
	if s.UpgradeCompleted.IsZero() {
		t.Error("UpgradeCompleted should be set")
	}
}

func TestDefaultStatePath(t *testing.T) {
	path := DefaultStatePath()
	if path == "" {
		t.Error("DefaultStatePath() returned empty string")
	}
}

// ---------------------------------------------------------------------------
// VersionChecker
// ---------------------------------------------------------------------------

func TestVersionChecker_StartStop(t *testing.T) {
	vc := &VersionChecker{
		currentVersion: "1.0.0",
		checkInterval:  1 * time.Hour,
		stopCh:         make(chan struct{}),
		isHomebrew:     true, // skip real HTTP calls
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vc.Start(ctx)

	// Double start should be no-op
	vc.Start(ctx)

	// Give goroutine time to start
	time.Sleep(50 * time.Millisecond)

	vc.Stop()

	// Double stop should be no-op
	vc.Stop()
}

func TestVersionChecker_GetLatestInfo(t *testing.T) {
	vc := &VersionChecker{
		currentVersion: "1.0.0",
		checkInterval:  1 * time.Hour,
		stopCh:         make(chan struct{}),
	}

	if info := vc.GetLatestInfo(); info != nil {
		t.Error("GetLatestInfo() should return nil initially")
	}

	vc.mu.Lock()
	vc.latestInfo = &VersionInfo{Current: "1.0.0", Latest: "v2.0.0"}
	vc.mu.Unlock()

	info := vc.GetLatestInfo()
	if info == nil {
		t.Fatal("GetLatestInfo() returned nil after setting")
	}
	if info.Latest != "v2.0.0" {
		t.Errorf("Latest = %q, want %q", info.Latest, "v2.0.0")
	}
}

func TestVersionChecker_LastCheck(t *testing.T) {
	vc := &VersionChecker{
		currentVersion: "1.0.0",
		stopCh:         make(chan struct{}),
	}

	if !vc.LastCheck().IsZero() {
		t.Error("LastCheck() should be zero initially")
	}
}

func TestVersionChecker_IsHomebrew(t *testing.T) {
	vc := &VersionChecker{isHomebrew: true, homebrewErr: fmt.Errorf("homebrew detected")}

	if !vc.IsHomebrew() {
		t.Error("IsHomebrew() = false, want true")
	}
	if vc.GetHomebrewError() == nil {
		t.Error("GetHomebrewError() = nil, want error")
	}
}

func TestVersionChecker_CheckNow_Homebrew(t *testing.T) {
	vc := &VersionChecker{
		isHomebrew:  true,
		homebrewErr: fmt.Errorf("homebrew installation"),
	}

	_, err := vc.CheckNow(context.Background())
	if err == nil {
		t.Fatal("CheckNow() expected error for homebrew, got nil")
	}
}

func TestVersionChecker_OnUpdate(t *testing.T) {
	vc := &VersionChecker{
		currentVersion: "1.0.0",
		stopCh:         make(chan struct{}),
	}

	called := false
	vc.OnUpdate(func(info *VersionInfo) {
		called = true
	})

	vc.mu.RLock()
	cb := vc.onUpdate
	vc.mu.RUnlock()
	if cb == nil {
		t.Fatal("onUpdate callback not set")
	}
	cb(&VersionInfo{})
	if !called {
		t.Error("callback was not called")
	}
}

func TestVersionChecker_ContextCancellation(t *testing.T) {
	vc := &VersionChecker{
		currentVersion: "1.0.0",
		checkInterval:  50 * time.Millisecond,
		stopCh:         make(chan struct{}),
		isHomebrew:     true, // skip real HTTP
	}

	ctx, cancel := context.WithCancel(context.Background())
	vc.Start(ctx)

	// Cancel context should stop the checker
	cancel()
	time.Sleep(100 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// GracefulUpgrader / HotUpgradeConfig defaults
// ---------------------------------------------------------------------------

func TestDefaultUpgradeOptions(t *testing.T) {
	opts := DefaultUpgradeOptions()
	if !opts.WaitForTasks {
		t.Error("WaitForTasks should default to true")
	}
	if opts.TaskTimeout != 5*time.Minute {
		t.Errorf("TaskTimeout = %v, want 5m", opts.TaskTimeout)
	}
	if opts.Force {
		t.Error("Force should default to false")
	}
}

func TestDefaultHotUpgradeConfig(t *testing.T) {
	cfg := DefaultHotUpgradeConfig()
	if !cfg.WaitForTasks {
		t.Error("WaitForTasks should default to true")
	}
	if cfg.TaskTimeout != 2*time.Minute {
		t.Errorf("TaskTimeout = %v, want 2m", cfg.TaskTimeout)
	}
}
