package briefs

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerStartStop(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	tests := []struct {
		name           string
		config         *BriefConfig
		expectRunning  bool
		expectStartErr bool
	}{
		{
			name: "disabled scheduler does not start",
			config: &BriefConfig{
				Enabled:  false,
				Schedule: "0 9 * * *",
				Timezone: "UTC",
			},
			expectRunning:  false,
			expectStartErr: false,
		},
		{
			name: "enabled scheduler starts successfully",
			config: &BriefConfig{
				Enabled:  true,
				Schedule: "0 9 * * *",
				Timezone: "UTC",
			},
			expectRunning:  true,
			expectStartErr: false,
		},
		{
			name: "invalid cron schedule returns error",
			config: &BriefConfig{
				Enabled:  true,
				Schedule: "invalid cron",
				Timezone: "UTC",
			},
			expectRunning:  false,
			expectStartErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewGenerator(store, tt.config)
			delivery := NewDeliveryService(tt.config)
			scheduler := NewScheduler(generator, delivery, tt.config, nil, nil)

			ctx := context.Background()
			err := scheduler.Start(ctx)

			if tt.expectStartErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectStartErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if scheduler.IsRunning() != tt.expectRunning {
				t.Errorf("IsRunning() = %v, want %v", scheduler.IsRunning(), tt.expectRunning)
			}

			// Test Stop
			scheduler.Stop()
			if scheduler.IsRunning() {
				t.Error("scheduler still running after Stop()")
			}
		})
	}
}

func TestSchedulerDoubleStart(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *",
		Timezone: "UTC",
	}

	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config)
	scheduler := NewScheduler(generator, delivery, config, nil, nil)

	ctx := context.Background()

	// First start
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	defer scheduler.Stop()

	// Second start should be a no-op
	if err := scheduler.Start(ctx); err != nil {
		t.Errorf("second start returned error: %v", err)
	}

	if !scheduler.IsRunning() {
		t.Error("scheduler should still be running after double start")
	}
}

func TestSchedulerDoubleStop(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *",
		Timezone: "UTC",
	}

	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config)
	scheduler := NewScheduler(generator, delivery, config, nil, nil)

	ctx := context.Background()

	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// First stop
	scheduler.Stop()

	// Second stop should be a no-op (not panic)
	scheduler.Stop()

	if scheduler.IsRunning() {
		t.Error("scheduler should not be running after stops")
	}
}

func TestSchedulerNextRun(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "* * * * *", // Every minute for testing
		Timezone: "UTC",
	}

	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config)
	scheduler := NewScheduler(generator, delivery, config, nil, nil)

	// Before start, NextRun should return zero time
	nextRun := scheduler.NextRun()
	if !nextRun.IsZero() {
		t.Errorf("expected zero time before start, got %v", nextRun)
	}

	ctx := context.Background()
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer scheduler.Stop()

	// After start, NextRun should return a future time
	nextRun = scheduler.NextRun()
	if nextRun.IsZero() {
		t.Error("expected non-zero NextRun after start")
	}

	if nextRun.Before(time.Now()) {
		t.Errorf("NextRun %v should be in the future", nextRun)
	}
}

func TestSchedulerLastRun(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "* * * * *",
		Timezone: "UTC",
	}

	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config)
	scheduler := NewScheduler(generator, delivery, config, nil, nil)

	// Before start, LastRun should return zero time
	lastRun := scheduler.LastRun()
	if !lastRun.IsZero() {
		t.Errorf("expected zero time before start, got %v", lastRun)
	}

	ctx := context.Background()
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer scheduler.Stop()

	// Initially, LastRun should be zero (no runs yet)
	lastRun = scheduler.LastRun()
	if !lastRun.IsZero() {
		t.Errorf("expected zero LastRun initially, got %v", lastRun)
	}
}

func TestSchedulerStatus(t *testing.T) {
	store, cleanup := setupSchedulerTestStore(t)
	defer cleanup()

	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * 1-5",
		Timezone: "America/New_York",
	}

	generator := NewGenerator(store, config)
	delivery := NewDeliveryService(config)
	scheduler := NewScheduler(generator, delivery, config, nil, nil)

	// Status before start
	status := scheduler.Status()
	if !status.Enabled {
		t.Error("status.Enabled should be true")
	}
	if status.Running {
		t.Error("status.Running should be false before start")
	}
	if status.Schedule != "0 9 * * 1-5" {
		t.Errorf("unexpected schedule: %s", status.Schedule)
	}
	if status.Timezone != "America/New_York" {
		t.Errorf("unexpected timezone: %s", status.Timezone)
	}

	// Start scheduler
	ctx := context.Background()
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer scheduler.Stop()

	// Status after start
	status = scheduler.Status()
	if !status.Running {
		t.Error("status.Running should be true after start")
	}
	if status.NextRun.IsZero() {
		t.Error("status.NextRun should not be zero after start")
	}
}
