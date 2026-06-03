package memory

import (
	"os"
	"testing"
	"time"
)

// recordTaskEvents inserts n task usage events for a user so CheckUsageThresholds
// has month-current cost ($1/task) and task-count data to evaluate. Timestamps
// are set to now (inside the current calendar month) so the month-start window
// in CheckUsageThresholds always includes them.
func recordTaskEvents(t *testing.T, store *Store, userID string, n int) {
	t.Helper()
	now := time.Now()
	for i := 0; i < n; i++ {
		evt := &UsageEvent{
			ID:        userID + "-task-" + itoa(i),
			Timestamp: now,
			UserID:    userID,
			ProjectID: "proj",
			EventType: EventTypeTask,
			Quantity:  1,
			UnitCost:  PricePerTask,
			TotalCost: PricePerTask,
		}
		if err := store.RecordUsageEvent(evt); err != nil {
			t.Fatalf("RecordUsageEvent failed: %v", err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestCheckUsageThresholds_Boundaries exercises the exact $100 cost and 500-task
// boundaries. The thresholds use strict greater-than (> 100.0, > 500), so the
// boundary value itself must NOT alert; one unit over must alert. Each task is
// $1, so task count and cost move together — N tasks => $N and N tasks.
func TestCheckUsageThresholds_Boundaries(t *testing.T) {
	tests := []struct {
		name      string
		tasks     int
		wantCost  bool // expect "Monthly cost threshold exceeded" alert
		wantTasks bool // expect "Monthly task limit approaching" alert
	}{
		{name: "well under both", tasks: 50, wantCost: false, wantTasks: false},
		{name: "exactly at cost boundary (100 tasks = $100)", tasks: 100, wantCost: false, wantTasks: false},
		{name: "one over cost boundary ($101)", tasks: 101, wantCost: true, wantTasks: false},
		{name: "between cost and task thresholds (300 tasks = $300)", tasks: 300, wantCost: true, wantTasks: false},
		{name: "exactly at task boundary (500 tasks = $500)", tasks: 500, wantCost: true, wantTasks: false},
		{name: "one over task boundary (501 tasks = $501)", tasks: 501, wantCost: true, wantTasks: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "threshold-gap-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			store, err := NewStore(tmpDir)
			if err != nil {
				t.Fatalf("NewStore failed: %v", err)
			}
			defer func() { _ = store.Close() }()

			userID := "boundary-user"
			recordTaskEvents(t, store, userID, tt.tasks)

			alerts, err := store.CheckUsageThresholds(userID)
			if err != nil {
				t.Fatalf("CheckUsageThresholds failed: %v", err)
			}

			gotCost := containsSubstr(alerts, "cost threshold exceeded")
			gotTasks := containsSubstr(alerts, "task limit approaching")

			if gotCost != tt.wantCost {
				t.Errorf("cost alert = %v, want %v (alerts=%v)", gotCost, tt.wantCost, alerts)
			}
			if gotTasks != tt.wantTasks {
				t.Errorf("task alert = %v, want %v (alerts=%v)", gotTasks, tt.wantTasks, alerts)
			}
		})
	}
}

func containsSubstr(alerts []string, sub string) bool {
	for _, a := range alerts {
		if contains(a, sub) {
			return true
		}
	}
	return false
}

// insertExecutionAt saves an execution via SaveExecution (which populates all
// non-nullable columns) then backdates created_at, since SaveExecution relies on
// the DB CURRENT_TIMESTAMP default and gives no seam to set it directly. The
// created_at is bound as a time.Time so it serializes identically to the query's
// bound Start/End values (avoids a UTC-vs-local string mismatch).
func insertExecutionAt(t *testing.T, store *Store, id, project string, createdAt time.Time) {
	t.Helper()
	if err := store.SaveExecution(&Execution{
		ID:          id,
		TaskID:      "task-" + id,
		ProjectPath: project,
		Status:      "completed",
	}); err != nil {
		t.Fatalf("SaveExecution %q failed: %v", id, err)
	}
	if _, err := store.db.Exec(`UPDATE executions SET created_at = ? WHERE id = ?`, createdAt, id); err != nil {
		t.Fatalf("backdate created_at for %q failed: %v", id, err)
	}
}

// TestGetExecutionsInPeriod_ProjectFilterINClause asserts the IN-clause path
// filters by project exactly (not just a lower bound) and that the time window
// is respected for the filtered query. The existing TestGetExecutionsInPeriod
// only checks wantMin and never proves non-matching projects are excluded.
func TestGetExecutionsInPeriod_ProjectFilterINClause(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "execperiod-gap-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Now()
	inWindow := now.Add(-1 * time.Hour)
	outsideWindow := now.Add(-72 * time.Hour)

	// In-window rows across three projects.
	insertExecutionAt(t, store, "a1", "/project/a", inWindow)
	insertExecutionAt(t, store, "a2", "/project/a", inWindow)
	insertExecutionAt(t, store, "b1", "/project/b", inWindow)
	insertExecutionAt(t, store, "c1", "/project/c", inWindow)
	// An in-window /project/a row that must be excluded by the time window below.
	insertExecutionAt(t, store, "a_old", "/project/a", outsideWindow)

	start := now.Add(-2 * time.Hour)
	end := now.Add(1 * time.Hour)

	t.Run("single project excludes others and respects window", func(t *testing.T) {
		got, err := store.GetExecutionsInPeriod(BriefQuery{
			Start:    start,
			End:      end,
			Projects: []string{"/project/a"},
		})
		if err != nil {
			t.Fatalf("GetExecutionsInPeriod failed: %v", err)
		}
		// Only a1 and a2 — a_old is outside window, b1/c1 are other projects.
		if len(got) != 2 {
			t.Fatalf("got %d executions, want exactly 2", len(got))
		}
		for _, e := range got {
			if e.ProjectPath != "/project/a" {
				t.Errorf("unexpected project %q in single-project result", e.ProjectPath)
			}
			if e.ID == "a_old" {
				t.Error("out-of-window row a_old leaked into result")
			}
		}
	})

	t.Run("two-project IN clause includes both, excludes third", func(t *testing.T) {
		got, err := store.GetExecutionsInPeriod(BriefQuery{
			Start:    start,
			End:      end,
			Projects: []string{"/project/a", "/project/b"},
		})
		if err != nil {
			t.Fatalf("GetExecutionsInPeriod failed: %v", err)
		}
		// a1, a2, b1 — not c1, not a_old.
		if len(got) != 3 {
			t.Fatalf("got %d executions, want exactly 3", len(got))
		}
		for _, e := range got {
			if e.ProjectPath == "/project/c" {
				t.Errorf("/project/c leaked into two-project IN result")
			}
			if e.ID == "a_old" {
				t.Error("out-of-window row a_old leaked into result")
			}
		}
	})

	t.Run("non-matching project returns empty", func(t *testing.T) {
		got, err := store.GetExecutionsInPeriod(BriefQuery{
			Start:    start,
			End:      end,
			Projects: []string{"/project/zzz"},
		})
		if err != nil {
			t.Fatalf("GetExecutionsInPeriod failed: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %d executions for unknown project, want 0", len(got))
		}
	})
}

// TestMigrateIdempotency verifies migrate() is safe to run twice on a live DB:
// the second call must not error (duplicate-column ALTERs are swallowed,
// CREATE ... IF NOT EXISTS is a no-op) and must preserve existing rows.
func TestMigrateIdempotency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "migrate-gap-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// NewStore already runs migrate() once.
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed a row populating an ALTER-added column (tokens_total) to prove data
	// survives a re-migrate.
	exec := &Execution{
		ID:          "migrate-row",
		TaskID:      "T-mig",
		ProjectPath: "/p",
		Status:      "completed",
		TokensTotal: 1234,
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution failed: %v", err)
	}

	// Second migrate() on the same live DB must be a no-op, not an error.
	if err := store.migrate(); err != nil {
		t.Fatalf("second migrate() failed: %v", err)
	}
	// Third run for good measure.
	if err := store.migrate(); err != nil {
		t.Fatalf("third migrate() failed: %v", err)
	}

	// Row and its ALTER-added columns must be intact.
	got, err := store.GetExecution("migrate-row")
	if err != nil {
		t.Fatalf("GetExecution after re-migrate failed: %v", err)
	}
	if got.TokensTotal != 1234 {
		t.Errorf("TokensTotal = %d, want 1234 after re-migrate", got.TokensTotal)
	}

	// A fresh write must still succeed against the re-migrated schema.
	if err := store.SaveExecution(&Execution{
		ID:          "migrate-row-2",
		TaskID:      "T-mig-2",
		ProjectPath: "/p",
		Status:      "queued",
	}); err != nil {
		t.Fatalf("SaveExecution after re-migrate failed: %v", err)
	}
}
