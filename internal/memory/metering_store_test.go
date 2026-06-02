package memory

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMeteringStore(t *testing.T) {
	// Create temporary directory for test database
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

	t.Run("RecordUsageEvent", func(t *testing.T) {
		event := &UsageEvent{
			ID:          "evt_test_001",
			Timestamp:   time.Now(),
			UserID:      "user_123",
			ProjectID:   "proj_456",
			EventType:   EventTypeTask,
			Quantity:    1,
			UnitCost:    PricePerTask,
			TotalCost:   PricePerTask,
			ExecutionID: "exec_789",
		}

		err := store.RecordUsageEvent(event)
		if err != nil {
			t.Errorf("RecordUsageEvent() error = %v", err)
		}
	})

	t.Run("RecordTaskUsage", func(t *testing.T) {
		err := store.RecordTaskUsage(
			"exec_001", // executionID
			"user_123", // userID
			"proj_456", // projectID
			120000,     // durationMs (2 minutes)
			10000,      // tokensInput
			5000,       // tokensOutput
		)
		if err != nil {
			t.Errorf("RecordTaskUsage() error = %v", err)
		}
	})

	t.Run("GetUsageSummary", func(t *testing.T) {
		// Record some test data
		for i := 0; i < 3; i++ {
			err := store.RecordTaskUsage(
				"exec_summary_"+string(rune('a'+i)),
				"user_summary",
				"proj_summary",
				60000,
				5000,
				2500,
			)
			if err != nil {
				t.Fatalf("Failed to record test data: %v", err)
			}
		}

		// Query summary
		query := UsageQuery{
			UserID: "user_summary",
			Start:  time.Now().Add(-24 * time.Hour),
			End:    time.Now().Add(time.Hour),
		}

		summary, err := store.GetUsageSummary(query)
		if err != nil {
			t.Errorf("GetUsageSummary() error = %v", err)
			return
		}

		if summary.TaskCount != 3 {
			t.Errorf("GetUsageSummary() TaskCount = %d, want 3", summary.TaskCount)
		}

		if summary.TaskCost < 3.0 {
			t.Errorf("GetUsageSummary() TaskCost = %f, want >= 3.0", summary.TaskCost)
		}

		if summary.TotalCost < summary.TaskCost {
			t.Errorf("GetUsageSummary() TotalCost (%f) < TaskCost (%f)", summary.TotalCost, summary.TaskCost)
		}
	})

	t.Run("GetUsageByProject", func(t *testing.T) {
		query := UsageQuery{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now().Add(time.Hour),
		}

		usage, err := store.GetUsageByProject(query)
		if err != nil {
			t.Errorf("GetUsageByProject() error = %v", err)
			return
		}

		if len(usage) == 0 {
			t.Error("GetUsageByProject() returned empty results, want at least 1 project")
		}
	})

	t.Run("GetUsageEvents", func(t *testing.T) {
		query := UsageQuery{
			Start: time.Now().Add(-24 * time.Hour),
			End:   time.Now().Add(time.Hour),
		}

		events, err := store.GetUsageEvents(query, 100)
		if err != nil {
			t.Errorf("GetUsageEvents() error = %v", err)
			return
		}

		if len(events) == 0 {
			t.Error("GetUsageEvents() returned empty results")
		}
	})

	t.Run("GetDailyUsage", func(t *testing.T) {
		query := UsageQuery{
			Start: time.Now().Add(-7 * 24 * time.Hour),
			End:   time.Now().Add(time.Hour),
		}

		daily, err := store.GetDailyUsage(query)
		if err != nil {
			t.Errorf("GetDailyUsage() error = %v", err)
			return
		}

		// Should have at least today's data
		if len(daily) == 0 {
			t.Error("GetDailyUsage() returned empty results")
		}
	})
}

func TestRecordUsageEvent_Metadata(t *testing.T) {
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

	event := &UsageEvent{
		ID:          "evt_meta_001",
		Timestamp:   time.Now(),
		UserID:      "user_meta",
		ProjectID:   "proj_meta",
		EventType:   EventTypeToken,
		Quantity:    15000,
		UnitCost:    0.001,
		TotalCost:   15.0,
		ExecutionID: "exec_meta",
		Metadata: map[string]interface{}{
			"input_tokens":  10000,
			"output_tokens": 5000,
			"model":         "claude-sonnet-4-6",
		},
	}

	err = store.RecordUsageEvent(event)
	if err != nil {
		t.Errorf("RecordUsageEvent() with metadata error = %v", err)
	}

	// Retrieve and verify
	query := UsageQuery{
		UserID: "user_meta",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now().Add(1 * time.Hour),
	}

	events, err := store.GetUsageEvents(query, 10)
	if err != nil {
		t.Fatalf("GetUsageEvents failed: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	found := false
	for _, e := range events {
		if e.ID == "evt_meta_001" {
			found = true
			if e.Metadata == nil {
				t.Error("metadata should not be nil")
			}
			break
		}
	}

	if !found {
		t.Error("could not find the recorded event")
	}
}

func TestRecordTaskUsage_ZeroTokens(t *testing.T) {
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

	// Should not fail with zero tokens
	err = store.RecordTaskUsage("exec_zero_tokens", "user_zero", "proj_zero", 60000, 0, 0)
	if err != nil {
		t.Errorf("RecordTaskUsage() with zero tokens error = %v", err)
	}

	// Verify only task and compute events were recorded (no token event)
	query := UsageQuery{
		UserID: "user_zero",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now().Add(1 * time.Hour),
	}

	events, _ := store.GetUsageEvents(query, 100)
	hasTokenEvent := false
	for _, e := range events {
		if e.EventType == EventTypeToken {
			hasTokenEvent = true
			break
		}
	}

	if hasTokenEvent {
		t.Error("should not record token event when tokens are zero")
	}
}

func TestRecordTaskUsage_ZeroDuration(t *testing.T) {
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

	// Should not fail with zero duration
	err = store.RecordTaskUsage("exec_zero_dur", "user_zero_dur", "proj_zero_dur", 0, 1000, 500)
	if err != nil {
		t.Errorf("RecordTaskUsage() with zero duration error = %v", err)
	}

	// Verify no compute event was recorded
	query := UsageQuery{
		UserID: "user_zero_dur",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now().Add(1 * time.Hour),
	}

	events, _ := store.GetUsageEvents(query, 100)
	hasComputeEvent := false
	for _, e := range events {
		if e.EventType == EventTypeCompute {
			hasComputeEvent = true
			break
		}
	}

	if hasComputeEvent {
		t.Error("should not record compute event when duration is zero")
	}
}

func TestGetUsageEvents_InvalidMetadataJSON(t *testing.T) {
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

	// Insert usage event with invalid metadata JSON directly into DB
	_, err = store.db.Exec(`
		INSERT INTO usage_events (id, timestamp, user_id, project_id, event_type, quantity, unit_cost, total_cost, metadata, execution_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "evt-invalid", time.Now(), "user-test", "proj-test", "task", 1.0, 0.5, 0.5, "invalid{json", "exec-1")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Capture slog output
	var buf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(oldLogger)

	query := UsageQuery{
		UserID: "user-test",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now().Add(1 * time.Hour),
	}

	events, err := store.GetUsageEvents(query, 100)
	if err != nil {
		t.Errorf("GetUsageEvents should not error on invalid metadata JSON: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if events[0].Metadata != nil {
		t.Errorf("Metadata should be nil after unmarshal failure")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to unmarshal usage event metadata") {
		t.Errorf("expected warning log about unmarshal failure, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "evt-invalid") {
		t.Errorf("expected event ID in log, got: %s", logOutput)
	}
}
