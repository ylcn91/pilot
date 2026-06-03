package dashboard

import (
	"math"
	"os"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

// TestStoreRefreshCmd_CostPerTaskZeroTasks verifies the TotalTasks==0 guard in
// storeRefreshCmd: with no executions the division branch is skipped so
// CostPerTask stays 0 (no division by zero / NaN).
func TestStoreRefreshCmd_CostPerTaskZeroTasks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-dash-cost-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// No executions saved → TotalTasks==0 → CostPerTask must remain 0.
	cmd := storeRefreshCmd(store)
	msg, ok := cmd().(storeRefreshMsg)
	if !ok {
		t.Fatalf("expected storeRefreshMsg")
	}

	if msg.metricsCard.TotalTasks != 0 {
		t.Fatalf("TotalTasks = %d, want 0", msg.metricsCard.TotalTasks)
	}
	if msg.metricsCard.CostPerTask != 0 {
		t.Errorf("CostPerTask = %v, want 0 when TotalTasks==0", msg.metricsCard.CostPerTask)
	}
	if math.IsNaN(msg.metricsCard.CostPerTask) || math.IsInf(msg.metricsCard.CostPerTask, 0) {
		t.Errorf("CostPerTask is NaN/Inf (%v): zero-task division was not guarded", msg.metricsCard.CostPerTask)
	}
}

// TestStoreRefreshCmd_CostPerTaskComputed verifies the positive branch:
// with TotalTasks>0, CostPerTask = TotalCostUSD / TotalTasks.
func TestStoreRefreshCmd_CostPerTaskComputed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-dash-cost2-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Two completed executions, total cost $3.00 → CostPerTask == 1.50.
	execs := []struct {
		id   string
		cost float64
	}{
		{"exec-1", 1.00},
		{"exec-2", 2.00},
	}
	for _, e := range execs {
		if err := store.SaveExecution(&memory.Execution{
			ID: e.id, TaskID: "TASK-" + e.id, ProjectPath: "/test", Status: "completed",
		}); err != nil {
			t.Fatalf("SaveExecution %s: %v", e.id, err)
		}
		if err := store.SaveExecutionMetrics(&memory.ExecutionMetrics{
			ExecutionID: e.id, TokensInput: 1000, TokensOutput: 500,
			TokensTotal: 1500, EstimatedCostUSD: e.cost,
		}); err != nil {
			t.Fatalf("SaveExecutionMetrics %s: %v", e.id, err)
		}
	}

	cmd := storeRefreshCmd(store)
	msg, ok := cmd().(storeRefreshMsg)
	if !ok {
		t.Fatalf("expected storeRefreshMsg")
	}

	if msg.metricsCard.TotalTasks != 2 {
		t.Fatalf("TotalTasks = %d, want 2", msg.metricsCard.TotalTasks)
	}
	want := msg.metricsCard.TotalCostUSD / 2.0
	if math.Abs(msg.metricsCard.CostPerTask-want) > 1e-9 {
		t.Errorf("CostPerTask = %v, want %v", msg.metricsCard.CostPerTask, want)
	}
}
