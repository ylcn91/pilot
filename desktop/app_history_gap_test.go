package main

import (
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestGetHistory_DedupKeepsBest verifies GetHistory deduplicates by TaskID,
// keeping the best execution per issue (completed beats failed, then prefers a
// PR URL). This exercises historyEntryBetter through the real store path.
func TestGetHistory_DedupKeepsBest(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	// Two executions for the same TaskID: an earlier failed attempt and a later
	// completed attempt with a PR URL. Dedup must keep the completed one.
	failedAt := now.Add(-2 * time.Hour)
	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-fail", TaskID: "GH-100", TaskTitle: "Retry task",
		ProjectPath: "/test", Status: "failed", CompletedAt: &failedAt,
	}); err != nil {
		t.Fatalf("SaveExecution failed-attempt: %v", err)
	}
	doneAt := now.Add(-1 * time.Hour)
	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-done", TaskID: "GH-100", TaskTitle: "Retry task",
		ProjectPath: "/test", Status: "completed", PRUrl: "https://github.com/x/y/pull/7",
		CompletedAt: &doneAt,
	}); err != nil {
		t.Fatalf("SaveExecution done-attempt: %v", err)
	}

	// A second, distinct task that should also appear.
	otherAt := now.Add(-30 * time.Minute)
	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-other", TaskID: "GH-200", TaskTitle: "Other task",
		ProjectPath: "/test", Status: "completed", CompletedAt: &otherAt,
	}); err != nil {
		t.Fatalf("SaveExecution other: %v", err)
	}

	app := &App{store: store}
	history := app.GetHistory(10)

	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2 (deduped per TaskID)", len(history))
	}

	byIssue := map[string]HistoryEntry{}
	for _, h := range history {
		byIssue[h.IssueID] = h
	}

	gh100, ok := byIssue["GH-100"]
	if !ok {
		t.Fatalf("GH-100 missing from history: %+v", history)
	}
	if gh100.Status != "completed" {
		t.Errorf("GH-100 kept status = %q, want completed (best beats failed)", gh100.Status)
	}
	if gh100.PRURL != "https://github.com/x/y/pull/7" {
		t.Errorf("GH-100 kept PRURL = %q, want the completed attempt's PR", gh100.PRURL)
	}
	if _, ok := byIssue["GH-200"]; !ok {
		t.Errorf("GH-200 missing from history: %+v", history)
	}
}

// TestGetHistory_SkipsNonTerminal verifies GetHistory ignores executions that
// are neither completed nor failed (running/queued/pending).
func TestGetHistory_SkipsNonTerminal(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	doneAt := now.Add(-time.Minute)

	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-run", TaskID: "GH-1", TaskTitle: "Running", ProjectPath: "/test", Status: "running",
	}); err != nil {
		t.Fatalf("SaveExecution running: %v", err)
	}
	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-done", TaskID: "GH-2", TaskTitle: "Done", ProjectPath: "/test",
		Status: "completed", CompletedAt: &doneAt,
	}); err != nil {
		t.Fatalf("SaveExecution completed: %v", err)
	}

	app := &App{store: store}
	history := app.GetHistory(10)

	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1 (running excluded)", len(history))
	}
	if history[0].IssueID != "GH-2" {
		t.Errorf("history[0].IssueID = %q, want GH-2", history[0].IssueID)
	}
}

func TestGetHistory_NilStore(t *testing.T) {
	app := &App{store: nil}
	if got := app.GetHistory(5); got != nil {
		t.Errorf("GetHistory with nil store = %v, want nil", got)
	}
}

// TestGetLogs_OldestFirst verifies GetLogs reverses the DESC store result so the
// returned slice is oldest-first (panel auto-scrolls to bottom).
func TestGetLogs_OldestFirst(t *testing.T) {
	store := newTestStore(t)

	base := time.Now().Add(-time.Hour)
	// Save in chronological order; each later than the previous.
	for i, msg := range []string{"first", "second", "third"} {
		if err := store.SaveLogEntry(&memory.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Level:     "info",
			Message:   msg,
			Component: "executor",
		}); err != nil {
			t.Fatalf("SaveLogEntry %s: %v", msg, err)
		}
	}

	app := &App{store: store}
	logs := app.GetLogs(10)

	if len(logs) != 3 {
		t.Fatalf("logs len = %d, want 3", len(logs))
	}
	want := []string{"first", "second", "third"}
	for i, w := range want {
		if logs[i].Message != w {
			t.Errorf("logs[%d].Message = %q, want %q (oldest-first ordering)", i, logs[i].Message, w)
		}
	}
}

func TestGetLogs_NilStore(t *testing.T) {
	app := &App{store: nil}
	if got := app.GetLogs(5); got != nil {
		t.Errorf("GetLogs with nil store = %v, want nil", got)
	}
}

func TestGetLogs_DefaultLimit(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveLogEntry(&memory.LogEntry{
		Timestamp: time.Now(), Level: "info", Message: "only", Component: "executor",
	}); err != nil {
		t.Fatalf("SaveLogEntry: %v", err)
	}
	app := &App{store: store}
	// limit<=0 defaults to 20 internally; with one row we still get one entry.
	logs := app.GetLogs(0)
	if len(logs) != 1 {
		t.Fatalf("logs len = %d, want 1", len(logs))
	}
	if logs[0].Message != "only" {
		t.Errorf("logs[0].Message = %q, want only", logs[0].Message)
	}
}
