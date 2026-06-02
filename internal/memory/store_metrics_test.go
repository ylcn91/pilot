package memory

import (
	"os"
	"testing"
	"time"
)

func TestGetExecutionsInPeriod(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Add some executions
	for i := 0; i < 5; i++ {
		exec := &Execution{
			ID:          "exec-period-" + string(rune('a'+i)),
			TaskID:      "TASK-" + string(rune('1'+i)),
			ProjectPath: "/project/a",
			Status:      "completed",
		}
		_ = store.SaveExecution(exec)
	}

	// Add execution for different project
	_ = store.SaveExecution(&Execution{
		ID:          "exec-other",
		TaskID:      "TASK-99",
		ProjectPath: "/project/b",
		Status:      "completed",
	})

	// Verify the executions were created
	allExecs, _ := store.GetRecentExecutions(100)
	t.Logf("Total executions in DB: %d", len(allExecs))

	tests := []struct {
		name    string
		query   BriefQuery
		wantMin int
	}{
		{
			name: "all projects",
			query: BriefQuery{
				Start: time.Now().Add(-24 * time.Hour),
				End:   time.Now().Add(24 * time.Hour),
			},
			wantMin: 6,
		},
		{
			name: "specific project",
			query: BriefQuery{
				Start:    time.Now().Add(-24 * time.Hour),
				End:      time.Now().Add(24 * time.Hour),
				Projects: []string{"/project/a"},
			},
			wantMin: 5,
		},
		{
			name: "multiple projects",
			query: BriefQuery{
				Start:    time.Now().Add(-24 * time.Hour),
				End:      time.Now().Add(24 * time.Hour),
				Projects: []string{"/project/a", "/project/b"},
			},
			wantMin: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.GetExecutionsInPeriod(tt.query)
			if err != nil {
				t.Fatalf("GetExecutionsInPeriod failed: %v", err)
			}

			if len(results) < tt.wantMin {
				t.Errorf("got %d executions, want at least %d", len(results), tt.wantMin)
			}
		})
	}
}

func TestGetBriefMetrics(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Add executions with various statuses
	executions := []*Execution{
		{ID: "metrics-1", TaskID: "T1", ProjectPath: "/p", Status: "completed", DurationMs: 1000, PRUrl: "https://github.com/a/b/pull/1"},
		{ID: "metrics-2", TaskID: "T2", ProjectPath: "/p", Status: "completed", DurationMs: 2000, PRUrl: ""},
		{ID: "metrics-3", TaskID: "T3", ProjectPath: "/p", Status: "failed", DurationMs: 500},
		{ID: "metrics-4", TaskID: "T4", ProjectPath: "/p", Status: "completed", DurationMs: 3000, PRUrl: "https://github.com/a/b/pull/2"},
	}

	for _, e := range executions {
		_ = store.SaveExecution(e)
	}

	query := BriefQuery{
		Start: time.Now().Add(-24 * time.Hour),
		End:   time.Now().Add(24 * time.Hour),
	}

	metrics, err := store.GetBriefMetrics(query)
	if err != nil {
		t.Fatalf("GetBriefMetrics failed: %v", err)
	}

	if metrics.TotalTasks < 4 {
		t.Errorf("TotalTasks = %d, want at least 4", metrics.TotalTasks)
	}
	if metrics.CompletedCount < 3 {
		t.Errorf("CompletedCount = %d, want at least 3", metrics.CompletedCount)
	}
	if metrics.FailedCount < 1 {
		t.Errorf("FailedCount = %d, want at least 1", metrics.FailedCount)
	}
	if metrics.PRsCreated < 2 {
		t.Errorf("PRsCreated = %d, want at least 2", metrics.PRsCreated)
	}
}

func TestGetLifetimeTokens(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Empty table should return zeros
	lt, err := store.GetLifetimeTokens()
	if err != nil {
		t.Fatalf("GetLifetimeTokens (empty): %v", err)
	}
	if lt.TotalTokens != 0 || lt.TotalCostUSD != 0 {
		t.Errorf("empty: want zeros, got tokens=%d cost=%.4f", lt.TotalTokens, lt.TotalCostUSD)
	}

	// Insert executions with token data
	execs := []struct {
		id     string
		input  int64
		output int64
		cost   float64
	}{
		{"exec-lt-1", 1000, 500, 0.05},
		{"exec-lt-2", 2000, 1000, 0.10},
		{"exec-lt-3", 3000, 1500, 0.15},
	}
	for _, e := range execs {
		if err := store.SaveExecution(&Execution{
			ID:          e.id,
			TaskID:      "TASK-" + e.id,
			ProjectPath: "/test",
			Status:      "completed",
		}); err != nil {
			t.Fatalf("SaveExecution %s: %v", e.id, err)
		}
		if err := store.SaveExecutionMetrics(&ExecutionMetrics{
			ExecutionID:      e.id,
			TokensInput:      e.input,
			TokensOutput:     e.output,
			TokensTotal:      e.input + e.output,
			EstimatedCostUSD: e.cost,
		}); err != nil {
			t.Fatalf("SaveExecutionMetrics %s: %v", e.id, err)
		}
	}

	lt, err = store.GetLifetimeTokens()
	if err != nil {
		t.Fatalf("GetLifetimeTokens: %v", err)
	}

	wantInput := int64(6000)
	wantOutput := int64(3000)
	wantTotal := int64(9000)
	wantCost := 0.30

	if lt.InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d", lt.InputTokens, wantInput)
	}
	if lt.OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %d, want %d", lt.OutputTokens, wantOutput)
	}
	if lt.TotalTokens != wantTotal {
		t.Errorf("TotalTokens = %d, want %d", lt.TotalTokens, wantTotal)
	}
	if lt.TotalCostUSD != wantCost {
		t.Errorf("TotalCostUSD = %.4f, want %.4f", lt.TotalCostUSD, wantCost)
	}
}

func TestGetLifetimeTaskCounts(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Empty table should return zeros
	tc, err := store.GetLifetimeTaskCounts()
	if err != nil {
		t.Fatalf("GetLifetimeTaskCounts (empty): %v", err)
	}
	if tc.Total != 0 || tc.Succeeded != 0 || tc.Failed != 0 {
		t.Errorf("empty: want zeros, got total=%d succeeded=%d failed=%d", tc.Total, tc.Succeeded, tc.Failed)
	}

	// Insert mix of completed and failed executions
	statuses := []struct {
		id     string
		status string
	}{
		{"exec-tc-1", "completed"},
		{"exec-tc-2", "completed"},
		{"exec-tc-3", "failed"},
		{"exec-tc-4", "completed"},
		{"exec-tc-5", "failed"},
	}
	for _, s := range statuses {
		if err := store.SaveExecution(&Execution{
			ID:          s.id,
			TaskID:      "TASK-" + s.id,
			ProjectPath: "/test",
			Status:      s.status,
		}); err != nil {
			t.Fatalf("SaveExecution %s: %v", s.id, err)
		}
	}

	tc, err = store.GetLifetimeTaskCounts()
	if err != nil {
		t.Fatalf("GetLifetimeTaskCounts: %v", err)
	}

	if tc.Total != 5 {
		t.Errorf("Total = %d, want 5", tc.Total)
	}
	if tc.Succeeded != 3 {
		t.Errorf("Succeeded = %d, want 3", tc.Succeeded)
	}
	if tc.Failed != 2 {
		t.Errorf("Failed = %d, want 2", tc.Failed)
	}
}

// TestRecentCompletedTelemetryStats verifies the zero-token telemetry gap
// query: rows are filtered to completed runs with a real commit, and rows
// without commit_sha (e.g. epic orchestrators) are excluded so they don't
// inflate the gap ratio. GH-2428.
func TestRecentCompletedTelemetryStats(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	type rec struct {
		id     string
		status string
		commit string
		tokens int64
	}
	rows := []rec{
		{"a", "completed", "deadbeef", 100}, // counts, not zero
		{"b", "completed", "cafe1234", 0},   // counts, zero
		{"c", "completed", "ba5eba11", 0},   // counts, zero
		{"d", "completed", "", 0},           // SKIPPED (no commit — epic orchestrator)
		{"e", "failed", "feedface", 0},      // SKIPPED (not completed)
	}
	for _, r := range rows {
		if err := store.SaveExecution(&Execution{
			ID:          r.id,
			TaskID:      "T-" + r.id,
			ProjectPath: "/x",
			Status:      r.status,
			CommitSHA:   r.commit,
		}); err != nil {
			t.Fatalf("SaveExecution %s: %v", r.id, err)
		}
		if err := store.SaveExecutionMetrics(&ExecutionMetrics{
			ExecutionID: r.id,
			TokensTotal: r.tokens,
		}); err != nil {
			t.Fatalf("SaveExecutionMetrics %s: %v", r.id, err)
		}
	}

	stats, err := store.RecentCompletedTelemetryStats(50)
	if err != nil {
		t.Fatalf("RecentCompletedTelemetryStats: %v", err)
	}
	if stats.CompletedRuns != 3 {
		t.Errorf("CompletedRuns = %d, want 3 (only completed+commit rows)", stats.CompletedRuns)
	}
	if stats.ZeroTokenRuns != 2 {
		t.Errorf("ZeroTokenRuns = %d, want 2", stats.ZeroTokenRuns)
	}
}

func TestGetLifetimeTokens_ExcludesZeroTokenRows(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Row with real tokens
	if err := store.SaveExecution(&Execution{ID: "exec-real", TaskID: "T-1", ProjectPath: "/p", Status: "completed"}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	if err := store.SaveExecutionMetrics(&ExecutionMetrics{ExecutionID: "exec-real", TokensInput: 5000, TokensOutput: 2000, TokensTotal: 7000, EstimatedCostUSD: 0.50}); err != nil {
		t.Fatalf("SaveExecutionMetrics: %v", err)
	}

	// Dispatcher queue / early-failure row with zero tokens
	if err := store.SaveExecution(&Execution{ID: "exec-zero", TaskID: "T-2", ProjectPath: "/p", Status: "failed"}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	lt, err := store.GetLifetimeTokens()
	if err != nil {
		t.Fatalf("GetLifetimeTokens: %v", err)
	}
	if lt.TotalTokens != 7000 {
		t.Errorf("TotalTokens = %d, want 7000 (zero-token row must be excluded)", lt.TotalTokens)
	}
	if lt.TotalCostUSD != 0.50 {
		t.Errorf("TotalCostUSD = %.4f, want 0.5000", lt.TotalCostUSD)
	}
}

func TestGetDailyMetrics_ExcludesZeroTokenRows(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)

	// Real execution
	if err := store.SaveExecution(&Execution{ID: "dm-real", TaskID: "T-1", ProjectPath: "/p", Status: "completed", CreatedAt: yesterday}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	if err := store.SaveExecutionMetrics(&ExecutionMetrics{ExecutionID: "dm-real", TokensInput: 3000, TokensOutput: 1000, TokensTotal: 4000, EstimatedCostUSD: 0.30}); err != nil {
		t.Fatalf("SaveExecutionMetrics: %v", err)
	}

	// Zero-token row (same day) — should not appear in daily metrics
	if err := store.SaveExecution(&Execution{ID: "dm-zero", TaskID: "T-2", ProjectPath: "/p", Status: "failed", CreatedAt: yesterday}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	q := MetricsQuery{Start: yesterday.Add(-time.Hour), End: now.Add(time.Hour)}
	days, err := store.GetDailyMetrics(q)
	if err != nil {
		t.Fatalf("GetDailyMetrics: %v", err)
	}
	if len(days) == 0 {
		t.Fatal("GetDailyMetrics: want at least 1 day row")
	}
	// Only the real execution should be counted
	if days[0].ExecutionCount != 1 {
		t.Errorf("ExecutionCount = %d, want 1 (zero-token row must be excluded)", days[0].ExecutionCount)
	}
}
