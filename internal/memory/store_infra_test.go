package memory

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStore_WithRetry(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	t.Run("succeeds on first attempt", func(t *testing.T) {
		attempts := 0
		err := store.withRetry("test", func() error {
			attempts++
			return nil
		})
		if err != nil {
			t.Errorf("withRetry should succeed: %v", err)
		}
		if attempts != 1 {
			t.Errorf("should only attempt once, got %d", attempts)
		}
	})

	t.Run("retries on database locked error", func(t *testing.T) {
		var buf bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		defer slog.SetDefault(oldLogger)

		attempts := 0
		err := store.withRetry("test", func() error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("database is locked (SQLITE_BUSY)")
			}
			return nil
		})
		if err != nil {
			t.Errorf("withRetry should succeed after retries: %v", err)
		}
		if attempts != 3 {
			t.Errorf("should retry until success, got %d attempts", attempts)
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, "Database locked, retrying") {
			t.Errorf("expected retry warning in logs, got: %s", logOutput)
		}
	})

	t.Run("does not retry non-retryable errors", func(t *testing.T) {
		attempts := 0
		err := store.withRetry("test", func() error {
			attempts++
			return fmt.Errorf("syntax error: invalid SQL")
		})
		if err == nil {
			t.Error("withRetry should return error")
		}
		if attempts != 1 {
			t.Errorf("should not retry non-retryable error, got %d attempts", attempts)
		}
		if !strings.Contains(err.Error(), "syntax error") {
			t.Errorf("should return original error, got: %v", err)
		}
	})

	t.Run("fails after max retries", func(t *testing.T) {
		attempts := 0
		err := store.withRetry("TestOp", func() error {
			attempts++
			return fmt.Errorf("database is locked (SQLITE_BUSY)")
		})
		if err == nil {
			t.Error("withRetry should return error after max retries")
		}
		if attempts != 5 {
			t.Errorf("should attempt 5 times, got %d", attempts)
		}
		if !strings.Contains(err.Error(), "TestOp failed after 5 retries") {
			t.Errorf("error should mention operation and retry count, got: %v", err)
		}
	})

	t.Run("retries on sqlite_locked", func(t *testing.T) {
		attempts := 0
		err := store.withRetry("test", func() error {
			attempts++
			if attempts < 2 {
				return fmt.Errorf("table is locked (SQLITE_LOCKED)")
			}
			return nil
		})
		if err != nil {
			t.Errorf("withRetry should succeed: %v", err)
		}
		if attempts != 2 {
			t.Errorf("should retry, got %d attempts", attempts)
		}
	})
}

func TestStore_ConnectionPoolSettings(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify connection pool settings by checking stats
	stats := store.db.Stats()

	// MaxOpenConns should be 1
	if stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
	}
}

func TestStore_SetApprovalRequestID_HappyPath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	exec := &Execution{
		ID:          "exec-approval-1",
		TaskID:      "GH-999",
		ProjectPath: "/proj",
		Status:      "completed",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	if err := store.SetApprovalRequestID(ctx, "GH-999", "req-abc"); err != nil {
		t.Fatalf("SetApprovalRequestID: %v", err)
	}

	// Verify via SetApprovalDecision — it matches on approval_request_id.
	if err := store.SetApprovalDecision(ctx, "req-abc", "approved", "tester"); err != nil {
		t.Fatalf("SetApprovalDecision after SetApprovalRequestID: %v", err)
	}

	got, err := store.GetExecution("exec-approval-1")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if got.ApprovalRequestID != "req-abc" {
		t.Errorf("ApprovalRequestID = %q, want %q", got.ApprovalRequestID, "req-abc")
	}
	if got.ApprovalDecision != "approved" {
		t.Errorf("ApprovalDecision = %q, want %q", got.ApprovalDecision, "approved")
	}
	if got.ApprovalDecisionBy != "tester" {
		t.Errorf("ApprovalDecisionBy = %q, want %q", got.ApprovalDecisionBy, "tester")
	}
}

func TestStore_SetApprovalRequestID_ZeroRowCase(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// No execution row exists for this task.
	err = store.SetApprovalRequestID(ctx, "GH-000", "req-xyz")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for missing task, got %v", err)
	}
}

func TestLogEntryCRUD(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Save entries
	entries := []*LogEntry{
		{ExecutionID: "exec-1", Timestamp: time.Now().Add(-2 * time.Second), Level: "info", Message: "Task started", Component: "executor"},
		{ExecutionID: "exec-1", Timestamp: time.Now().Add(-1 * time.Second), Level: "warn", Message: "Slow build", Component: "executor"},
		{ExecutionID: "exec-1", Timestamp: time.Now(), Level: "error", Message: "Build failed", Component: "executor"},
	}

	for _, e := range entries {
		if err := store.SaveLogEntry(e); err != nil {
			t.Fatalf("SaveLogEntry failed: %v", err)
		}
		if e.ID == 0 {
			t.Error("Expected non-zero ID after save")
		}
	}

	// Get recent logs
	recent, err := store.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("GetRecentLogs failed: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(recent))
	}

	// Should be ordered DESC by timestamp — most recent first
	if recent[0].Message != "Build failed" {
		t.Errorf("Expected most recent entry first, got %q", recent[0].Message)
	}
	if recent[0].Level != "error" {
		t.Errorf("Expected level 'error', got %q", recent[0].Level)
	}

	// Test limit
	limited, err := store.GetRecentLogs(2)
	if err != nil {
		t.Fatalf("GetRecentLogs with limit failed: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("Expected 2 entries with limit, got %d", len(limited))
	}

	// Empty result
	tmpDir2, _ := os.MkdirTemp("", "pilot-test-empty-*")
	defer func() { _ = os.RemoveAll(tmpDir2) }()
	store2, _ := NewStore(tmpDir2)
	defer func() { _ = store2.Close() }()

	empty, err := store2.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("GetRecentLogs on empty store failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected 0 entries on empty store, got %d", len(empty))
	}
}

func TestLogSubscribeLogs(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Subscribe before saving
	ch := store.SubscribeLogs()

	entry := &LogEntry{
		ExecutionID: "exec-sub",
		Timestamp:   time.Now(),
		Level:       "info",
		Message:     "hello subscriber",
		Component:   "test",
	}

	if err := store.SaveLogEntry(entry); err != nil {
		t.Fatalf("SaveLogEntry failed: %v", err)
	}

	select {
	case got := <-ch:
		if got.Message != "hello subscriber" {
			t.Errorf("Expected 'hello subscriber', got %q", got.Message)
		}
		if got.ID == 0 {
			t.Error("Expected non-zero ID on received entry")
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for subscriber notification")
	}

	// Unsubscribe and verify channel is closed
	store.UnsubscribeLogs(ch)

	// Save another entry — should not panic or block
	entry2 := &LogEntry{
		ExecutionID: "exec-sub",
		Timestamp:   time.Now(),
		Level:       "info",
		Message:     "after unsubscribe",
		Component:   "test",
	}
	if err := store.SaveLogEntry(entry2); err != nil {
		t.Fatalf("SaveLogEntry after unsubscribe failed: %v", err)
	}
}

func TestLogSubscribeMultipleSubscribers(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ch1 := store.SubscribeLogs()
	ch2 := store.SubscribeLogs()

	entry := &LogEntry{
		ExecutionID: "exec-multi",
		Timestamp:   time.Now(),
		Level:       "warn",
		Message:     "broadcast test",
		Component:   "test",
	}

	if err := store.SaveLogEntry(entry); err != nil {
		t.Fatalf("SaveLogEntry failed: %v", err)
	}

	// Both subscribers should receive the entry
	for i, ch := range []<-chan *LogEntry{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Message != "broadcast test" {
				t.Errorf("subscriber %d: expected 'broadcast test', got %q", i, got.Message)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}

	store.UnsubscribeLogs(ch1)
	store.UnsubscribeLogs(ch2)
}

// TestPruneExecutionLogs verifies D5: deletes logs older than the cutoff and
// leaves newer ones untouched.
func TestPruneExecutionLogs(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	old := time.Now().Add(-2 * time.Hour)
	recent := time.Now().Add(-10 * time.Minute)

	// Insert two old entries and one recent entry directly.
	_, err = store.db.Exec(`INSERT INTO execution_logs (timestamp, level, message, component) VALUES (?, 'info', 'old1', 'test')`, old)
	if err != nil {
		t.Fatalf("insert old1: %v", err)
	}
	_, err = store.db.Exec(`INSERT INTO execution_logs (timestamp, level, message, component) VALUES (?, 'info', 'old2', 'test')`, old)
	if err != nil {
		t.Fatalf("insert old2: %v", err)
	}
	_, err = store.db.Exec(`INSERT INTO execution_logs (timestamp, level, message, component) VALUES (?, 'info', 'recent', 'test')`, recent)
	if err != nil {
		t.Fatalf("insert recent: %v", err)
	}

	deleted, err := store.PruneExecutionLogs(time.Hour)
	if err != nil {
		t.Fatalf("PruneExecutionLogs: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM execution_logs`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("remaining rows = %d, want 1", count)
	}
}
