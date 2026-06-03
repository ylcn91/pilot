package autopilot

import (
	"log/slog"
	"os"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

// newTestStore creates a temp SQLite-backed memory.Store for restore tests.
func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "metrics-restore-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	})
	return store
}

// TestMetricsRestoreFromRow asserts RestoreFromRow seeds the token/cost/execution
// counters from a persisted snapshot, parsing the composite "model|direction" /
// "model|result" storage keys back into their typed buckets.
func TestMetricsRestoreFromRow(t *testing.T) {
	m := NewMetrics()

	row := &memory.AutopilotMetricsRow{
		TokensConsumed: map[string]int64{
			"claude-opus|input":  1200,
			"claude-opus|output": 340,
		},
		ExecutionCostUSD: map[string]float64{
			"claude-opus": 4.25,
		},
		ExecutionsByResult: map[string]int64{
			"claude-opus|success": 7,
			"claude-opus|failed":  2,
		},
	}

	m.RestoreFromRow(row)
	snap := m.Snapshot()

	if got := snap.TokensConsumed[tokenKey{Model: "claude-opus", Direction: "input"}]; got != 1200 {
		t.Errorf("input tokens: want 1200, got %d", got)
	}
	if got := snap.TokensConsumed[tokenKey{Model: "claude-opus", Direction: "output"}]; got != 340 {
		t.Errorf("output tokens: want 340, got %d", got)
	}
	if got := snap.ExecutionCostUSD["claude-opus"]; got != 4.25 {
		t.Errorf("cost: want 4.25, got %v", got)
	}
	if got := snap.ExecutionsByResult[execKey{Model: "claude-opus", Result: "success"}]; got != 7 {
		t.Errorf("success executions: want 7, got %d", got)
	}
	if got := snap.ExecutionsByResult[execKey{Model: "claude-opus", Result: "failed"}]; got != 2 {
		t.Errorf("failed executions: want 2, got %d", got)
	}
}

// TestMetricsRestoreFromRowNilAndMalformed verifies RestoreFromRow is a no-op on
// nil and skips keys missing the "|" separator without corrupting valid entries.
func TestMetricsRestoreFromRowNilAndMalformed(t *testing.T) {
	m := NewMetrics()
	m.RestoreFromRow(nil)
	if len(m.Snapshot().TokensConsumed) != 0 {
		t.Fatal("nil row should not seed any counters")
	}

	m.RestoreFromRow(&memory.AutopilotMetricsRow{
		TokensConsumed:     map[string]int64{"no-separator": 99, "claude|input": 5},
		ExecutionsByResult: map[string]int64{"also-bad": 1},
	})
	snap := m.Snapshot()
	if _, ok := snap.TokensConsumed[tokenKey{Model: "claude", Direction: "input"}]; !ok {
		t.Error("well-formed token key should be restored")
	}
	if len(snap.TokensConsumed) != 1 {
		t.Errorf("malformed token key should be skipped, got %d entries", len(snap.TokensConsumed))
	}
	if len(snap.ExecutionsByResult) != 0 {
		t.Errorf("malformed execution key should be skipped, got %d entries", len(snap.ExecutionsByResult))
	}
}

// TestMetricsPersisterRestoreAfterRestart simulates a daemon restart: run 1
// accumulates counters and persists a snapshot, then a fresh Metrics (run 2)
// is seeded from that snapshot via the persister's restore() and continues
// accumulating on top — proving counter continuity (GH-2836).
func TestMetricsPersisterRestoreAfterRestart(t *testing.T) {
	store := newTestStore(t)

	// --- Run 1: accumulate and persist a snapshot. ---
	run1 := &Controller{metrics: NewMetrics()}
	run1.metrics.RecordTokens("claude-opus", "input", 1000)
	run1.metrics.RecordTokens("claude-opus", "output", 250)
	run1.metrics.RecordCost("claude-opus", 3.50)
	run1.metrics.RecordExecution("claude-opus", "success")
	run1.metrics.RecordExecution("claude-opus", "success")
	run1.metrics.RecordExecution("claude-opus", "failed")

	persister1 := &MetricsPersister{
		controller: run1,
		store:      store,
		log:        slog.Default(),
	}
	persister1.persist()

	// --- Run 2: fresh in-memory metrics (counters reset to zero on restart). ---
	run2 := &Controller{metrics: NewMetrics()}
	if got := run2.metrics.Snapshot().TokensConsumed[tokenKey{Model: "claude-opus", Direction: "input"}]; got != 0 {
		t.Fatalf("fresh metrics should start at zero, got %d", got)
	}

	persister2 := &MetricsPersister{
		controller: run2,
		store:      store,
		log:        slog.Default(),
	}
	persister2.restore()

	snap := run2.metrics.Snapshot()
	if got := snap.TokensConsumed[tokenKey{Model: "claude-opus", Direction: "input"}]; got != 1000 {
		t.Errorf("after restart input tokens: want 1000, got %d", got)
	}
	if got := snap.TokensConsumed[tokenKey{Model: "claude-opus", Direction: "output"}]; got != 250 {
		t.Errorf("after restart output tokens: want 250, got %d", got)
	}
	if got := snap.ExecutionCostUSD["claude-opus"]; got != 3.50 {
		t.Errorf("after restart cost: want 3.50, got %v", got)
	}
	if got := snap.ExecutionsByResult[execKey{Model: "claude-opus", Result: "success"}]; got != 2 {
		t.Errorf("after restart success execs: want 2, got %d", got)
	}

	// New executions accumulate on top of the restored baseline.
	run2.metrics.RecordTokens("claude-opus", "input", 500)
	run2.metrics.RecordExecution("claude-opus", "success")
	snap = run2.metrics.Snapshot()
	if got := snap.TokensConsumed[tokenKey{Model: "claude-opus", Direction: "input"}]; got != 1500 {
		t.Errorf("post-restore accumulation input tokens: want 1500, got %d", got)
	}
	if got := snap.ExecutionsByResult[execKey{Model: "claude-opus", Result: "success"}]; got != 3 {
		t.Errorf("post-restore accumulation success execs: want 3, got %d", got)
	}
}
