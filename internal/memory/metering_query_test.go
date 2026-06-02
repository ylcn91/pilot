package memory

import (
	"os"
	"testing"
	"time"
)

func TestGetUsageSummary_Empty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metering_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	query := UsageQuery{
		UserID: "nonexistent_user",
		Start:  time.Now().Add(-24 * time.Hour),
		End:    time.Now().Add(time.Hour),
	}

	summary, err := store.GetUsageSummary(query)
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	if summary.TaskCount != 0 {
		t.Errorf("TaskCount = %d, want 0 for empty result", summary.TaskCount)
	}
	if summary.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0 for empty result", summary.TotalCost)
	}
}

func TestGetUsageSummary_WithProjectFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metering_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Record usage for different projects
	_ = store.RecordTaskUsage("exec_proj_a", "user_filter", "proj_a", 60000, 5000, 2500)
	_ = store.RecordTaskUsage("exec_proj_a2", "user_filter", "proj_a", 60000, 5000, 2500)
	_ = store.RecordTaskUsage("exec_proj_b", "user_filter", "proj_b", 60000, 5000, 2500)

	// Query with project filter
	query := UsageQuery{
		UserID:    "user_filter",
		ProjectID: "proj_a",
		Start:     time.Now().Add(-1 * time.Hour),
		End:       time.Now().Add(1 * time.Hour),
	}

	summary, err := store.GetUsageSummary(query)
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	if summary.TaskCount != 2 {
		t.Errorf("TaskCount = %d, want 2 for proj_a only", summary.TaskCount)
	}
}

func TestGetUsageEvents_WithTypeFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metering_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Record various event types
	_ = store.RecordTaskUsage("exec_type_1", "user_type", "proj_type", 60000, 5000, 2500)

	query := UsageQuery{
		UserID:    "user_type",
		Start:     time.Now().Add(-1 * time.Hour),
		End:       time.Now().Add(1 * time.Hour),
		EventType: EventTypeTask,
	}

	events, err := store.GetUsageEvents(query, 100)
	if err != nil {
		t.Fatalf("GetUsageEvents failed: %v", err)
	}

	for _, e := range events {
		if e.EventType != EventTypeTask {
			t.Errorf("event type = %s, want %s", e.EventType, EventTypeTask)
		}
	}
}

func TestGetDailyUsage_MultipleTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metering_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Record multiple tasks today
	for i := 0; i < 3; i++ {
		_ = store.RecordTaskUsage(
			"exec_daily_"+string(rune('a'+i)),
			"user_daily",
			"proj_daily",
			60000,
			5000,
			2500,
		)
	}

	query := UsageQuery{
		UserID: "user_daily",
		Start:  time.Now().Add(-24 * time.Hour),
		End:    time.Now().Add(24 * time.Hour),
	}

	daily, err := store.GetDailyUsage(query)
	if err != nil {
		t.Fatalf("GetDailyUsage failed: %v", err)
	}

	if len(daily) == 0 {
		t.Fatal("expected at least one day of usage")
	}

	today := daily[0]
	if today.TaskCount != 3 {
		t.Errorf("TaskCount = %d, want 3", today.TaskCount)
	}
	if today.TotalCost <= 0 {
		t.Error("TotalCost should be positive")
	}
}

func TestCheckUsageThresholds(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metering_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Record many tasks to trigger threshold
	for i := 0; i < 150; i++ {
		_ = store.RecordTaskUsage(
			"exec_threshold_"+string(rune(i)),
			"user_threshold",
			"proj_threshold",
			60000,
			10000,
			5000,
		)
	}

	alerts, err := store.CheckUsageThresholds("user_threshold")
	if err != nil {
		t.Fatalf("CheckUsageThresholds failed: %v", err)
	}

	// Should have cost threshold alert (150 tasks * $1/task = $150 > $100)
	if len(alerts) == 0 {
		t.Error("expected at least one alert for high usage")
	}

	hasMonthlyAlert := false
	for _, alert := range alerts {
		if alert != "" {
			hasMonthlyAlert = true
			break
		}
	}

	if !hasMonthlyAlert {
		t.Log("Note: threshold alerts may depend on month boundaries")
	}
}
