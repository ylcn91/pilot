package memory

import (
	"os"
	"testing"
)

// TestEffortLevelColumns_MigrationAddsColumns verifies that a fresh DB created by NewStore
// has the effort_level and complexity_level columns in the executions table.
func TestEffortLevelColumns_MigrationAddsColumns(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Query sqlite_master to verify columns exist
	var effortCount, complexityCount int
	row := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('executions') WHERE name='effort_level'`)
	if err := row.Scan(&effortCount); err != nil {
		t.Fatalf("pragma_table_info effort_level: %v", err)
	}
	row = store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('executions') WHERE name='complexity_level'`)
	if err := row.Scan(&complexityCount); err != nil {
		t.Fatalf("pragma_table_info complexity_level: %v", err)
	}

	if effortCount != 1 {
		t.Errorf("effort_level column missing from executions table")
	}
	if complexityCount != 1 {
		t.Errorf("complexity_level column missing from executions table")
	}
}

// TestEffortLevelColumns_BackwardsCompat verifies that opening a DB that already has an
// executions table (but lacks effort_level/complexity_level) runs the migration cleanly
// via the idempotent ALTER TABLE pattern.
func TestEffortLevelColumns_BackwardsCompat(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a DB without the new columns
	store1, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore (first open): %v", err)
	}
	// Insert a row before the migration adds the columns; simulate a pre-existing row.
	_ = store1.SaveExecution(&Execution{
		ID: "pre-migration", TaskID: "T-pre", ProjectPath: "/p", Status: "completed",
	})
	_ = store1.Close()

	// Re-open: migration must run the ALTER TABLE statements idempotently.
	store2, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore (second open, post-migration): %v", err)
	}
	defer func() { _ = store2.Close() }()

	// Pre-existing row must still be readable with null columns coerced to "".
	exec, err := store2.GetExecution("pre-migration")
	if err != nil {
		t.Fatalf("GetExecution after migration: %v", err)
	}
	if exec.EffortLevel != "" {
		t.Errorf("pre-migration row: EffortLevel = %q, want empty", exec.EffortLevel)
	}
	if exec.ComplexityLevel != "" {
		t.Errorf("pre-migration row: ComplexityLevel = %q, want empty", exec.ComplexityLevel)
	}
}

// TestEffortLevelColumns_RoundTrip verifies that SaveExecution persists EffortLevel and
// ComplexityLevel, and GetExecution returns the same values.
func TestEffortLevelColumns_RoundTrip(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	exec := &Execution{
		ID:              "exec-effort-rt",
		TaskID:          "T-effort",
		ProjectPath:     "/project",
		Status:          "completed",
		EffortLevel:     "medium",
		ComplexityLevel: "simple",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	got, err := store.GetExecution("exec-effort-rt")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if got.EffortLevel != "medium" {
		t.Errorf("EffortLevel = %q, want %q", got.EffortLevel, "medium")
	}
	if got.ComplexityLevel != "simple" {
		t.Errorf("ComplexityLevel = %q, want %q", got.ComplexityLevel, "simple")
	}

	// Also verify UpdateExecutionEffort writes new values.
	if err := store.UpdateExecutionEffort("exec-effort-rt", "high", "complex"); err != nil {
		t.Fatalf("UpdateExecutionEffort: %v", err)
	}
	got2, err := store.GetExecution("exec-effort-rt")
	if err != nil {
		t.Fatalf("GetExecution after update: %v", err)
	}
	if got2.EffortLevel != "high" {
		t.Errorf("after update: EffortLevel = %q, want %q", got2.EffortLevel, "high")
	}
	if got2.ComplexityLevel != "complex" {
		t.Errorf("after update: ComplexityLevel = %q, want %q", got2.ComplexityLevel, "complex")
	}
}
