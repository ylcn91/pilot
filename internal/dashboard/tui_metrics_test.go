package dashboard

import (
	"math"
	"os"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestHydrateFromStore_LifetimeTokens(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-dash-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Insert executions with known token/cost data across "multiple days"
	execs := []struct {
		id     string
		input  int64
		output int64
		cost   float64
	}{
		{"exec-1", 10000, 5000, 0.50},
		{"exec-2", 20000, 10000, 1.00},
		{"exec-3", 30000, 15000, 1.50},
	}
	for _, e := range execs {
		if err := store.SaveExecution(&memory.Execution{
			ID:          e.id,
			TaskID:      "TASK-" + e.id,
			ProjectPath: "/test",
			Status:      "completed",
		}); err != nil {
			t.Fatalf("SaveExecution %s: %v", e.id, err)
		}
		if err := store.SaveExecutionMetrics(&memory.ExecutionMetrics{
			ExecutionID:      e.id,
			TokensInput:      e.input,
			TokensOutput:     e.output,
			TokensTotal:      e.input + e.output,
			EstimatedCostUSD: e.cost,
		}); err != nil {
			t.Fatalf("SaveExecutionMetrics %s: %v", e.id, err)
		}
	}

	// Create model — simulates a fresh restart (new session, empty token usage)
	m := NewModelWithStore("test", store)

	// Metrics card should reflect lifetime totals from executions, not session (zero)
	wantInput := 60000
	wantOutput := 30000
	wantTotal := 90000
	wantCost := 3.00

	if m.metricsCard.InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d", m.metricsCard.InputTokens, wantInput)
	}
	if m.metricsCard.OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %d, want %d", m.metricsCard.OutputTokens, wantOutput)
	}
	if m.metricsCard.TotalTokens != wantTotal {
		t.Errorf("TotalTokens = %d, want %d", m.metricsCard.TotalTokens, wantTotal)
	}
	if math.Abs(m.metricsCard.TotalCostUSD-wantCost) > 0.001 {
		t.Errorf("TotalCostUSD = %.4f, want %.4f", m.metricsCard.TotalCostUSD, wantCost)
	}
}

func TestUpdateTokensMsg_AddsToLifetimeTotals(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-dash-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed with historical execution data
	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-old", TaskID: "TASK-OLD", ProjectPath: "/test", Status: "completed",
	}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	if err := store.SaveExecutionMetrics(&memory.ExecutionMetrics{
		ExecutionID: "exec-old", TokensInput: 10000, TokensOutput: 5000,
		TokensTotal: 15000, EstimatedCostUSD: 1.00,
	}); err != nil {
		t.Fatalf("SaveExecutionMetrics: %v", err)
	}

	m := NewModelWithStore("test", store)

	// Simulate a token update from a running execution (cumulative: 2000 in, 1000 out)
	updated, _ := m.Update(updateTokensMsg{InputTokens: 2000, OutputTokens: 1000, TotalTokens: 3000})
	model := updated.(Model)

	// metricsCard should be lifetime (10000+2000=12000 input, 5000+1000=6000 output)
	if model.metricsCard.InputTokens != 12000 {
		t.Errorf("InputTokens = %d, want 12000", model.metricsCard.InputTokens)
	}
	if model.metricsCard.OutputTokens != 6000 {
		t.Errorf("OutputTokens = %d, want 6000", model.metricsCard.OutputTokens)
	}
	if model.metricsCard.TotalTokens != 18000 {
		t.Errorf("TotalTokens = %d, want 18000", model.metricsCard.TotalTokens)
	}
}

func TestAddCompletedTask_NewFieldsStored(t *testing.T) {
	m := NewModel("test")

	// Send a completed task with parentID and isEpic=false (sub-issue)
	msg := addCompletedTaskMsg(CompletedTask{
		ID:       "GH-575",
		Title:    "Sub-issue task",
		Status:   "success",
		Duration: "30s",
		ParentID: "GH-498",
		IsEpic:   false,
	})
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if len(model.completedTasks) != 1 {
		t.Fatalf("completedTasks len = %d, want 1", len(model.completedTasks))
	}
	task := model.completedTasks[0]
	if task.ParentID != "GH-498" {
		t.Errorf("ParentID = %q, want %q", task.ParentID, "GH-498")
	}
	if task.IsEpic {
		t.Error("IsEpic = true, want false")
	}

	// Send an epic task with SubIssues, TotalSubs, DoneSubs
	epicMsg := addCompletedTaskMsg(CompletedTask{
		ID:        "GH-498",
		Title:     "Epic decomposition task",
		Status:    "success",
		Duration:  "5m",
		IsEpic:    true,
		SubIssues: []string{"GH-575", "GH-576", "GH-577"},
		TotalSubs: 3,
		DoneSubs:  2,
	})
	updated, _ = model.Update(epicMsg)
	model = updated.(Model)

	if len(model.completedTasks) != 2 {
		t.Fatalf("completedTasks len = %d, want 2", len(model.completedTasks))
	}
	epic := model.completedTasks[1]
	if !epic.IsEpic {
		t.Error("IsEpic = false, want true")
	}
	if epic.TotalSubs != 3 {
		t.Errorf("TotalSubs = %d, want 3", epic.TotalSubs)
	}
	if epic.DoneSubs != 2 {
		t.Errorf("DoneSubs = %d, want 2", epic.DoneSubs)
	}
	if len(epic.SubIssues) != 3 {
		t.Fatalf("SubIssues len = %d, want 3", len(epic.SubIssues))
	}
	if epic.SubIssues[0] != "GH-575" || epic.SubIssues[1] != "GH-576" || epic.SubIssues[2] != "GH-577" {
		t.Errorf("SubIssues = %v, want [GH-575 GH-576 GH-577]", epic.SubIssues)
	}
}

func TestAddCompletedTask_BackwardCompatEmpty(t *testing.T) {
	m := NewModel("test")

	// Simulate the backward-compatible call (parentID="", isEpic=false)
	cmd := AddCompletedTask("GH-100", "Simple task", "success", "10s", "", false)
	msg := cmd().(addCompletedTaskMsg)
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if len(model.completedTasks) != 1 {
		t.Fatalf("completedTasks len = %d, want 1", len(model.completedTasks))
	}
	task := model.completedTasks[0]
	if task.ParentID != "" {
		t.Errorf("ParentID = %q, want empty", task.ParentID)
	}
	if task.IsEpic {
		t.Error("IsEpic = true, want false")
	}
	if task.TotalSubs != 0 {
		t.Errorf("TotalSubs = %d, want 0", task.TotalSubs)
	}
	if task.DoneSubs != 0 {
		t.Errorf("DoneSubs = %d, want 0", task.DoneSubs)
	}
	if task.SubIssues != nil {
		t.Errorf("SubIssues = %v, want nil", task.SubIssues)
	}
}

// TestStoreRefreshMsg_UpdatesHistoryAndMetrics verifies that storeRefreshMsg
// replaces stale in-memory history and metrics with live DB state (GH-2248).
func TestStoreRefreshMsg_UpdatesHistoryAndMetrics(t *testing.T) {
	m := NewModel("test")
	// Seed stale in-memory state
	m.completedTasks = []CompletedTask{
		{ID: "stale-1", Title: "Stale Task", Status: "failed"},
	}
	m.metricsCard = MetricsCardData{TotalTasks: 1, Failed: 1}

	// Simulate a store refresh with different data (as if the DB row was deleted)
	msg := storeRefreshMsg{
		completedTasks: []CompletedTask{
			{ID: "fresh-1", Title: "Fresh Task", Status: "success"},
			{ID: "fresh-2", Title: "Another Task", Status: "success"},
		},
		metricsCard: MetricsCardData{
			TotalTasks:  2,
			Succeeded:   2,
			Failed:      0,
			TotalTokens: 5000,
		},
	}

	updated, _ := m.Update(msg)
	model := updated.(Model)

	if len(model.completedTasks) != 2 {
		t.Fatalf("completedTasks len = %d, want 2", len(model.completedTasks))
	}
	if model.completedTasks[0].ID != "fresh-1" {
		t.Errorf("completedTasks[0].ID = %q, want %q", model.completedTasks[0].ID, "fresh-1")
	}
	if model.metricsCard.TotalTasks != 2 {
		t.Errorf("TotalTasks = %d, want 2", model.metricsCard.TotalTasks)
	}
	if model.metricsCard.Failed != 0 {
		t.Errorf("Failed = %d, want 0", model.metricsCard.Failed)
	}
}

// TestStoreRefreshCmd_QueriesDB verifies storeRefreshCmd returns correct data
// from SQLite (GH-2248).
func TestStoreRefreshCmd_QueriesDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-dash-refresh-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Insert a completed execution
	if err := store.SaveExecution(&memory.Execution{
		ID: "exec-1", TaskID: "TASK-1", TaskTitle: "Test Task",
		ProjectPath: "/test", Status: "completed",
	}); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}
	if err := store.SaveExecutionMetrics(&memory.ExecutionMetrics{
		ExecutionID: "exec-1", TokensInput: 1000, TokensOutput: 500,
		TokensTotal: 1500, EstimatedCostUSD: 0.10,
	}); err != nil {
		t.Fatalf("SaveExecutionMetrics: %v", err)
	}

	// Run the refresh command
	cmd := storeRefreshCmd(store)
	rawMsg := cmd()
	msg, ok := rawMsg.(storeRefreshMsg)
	if !ok {
		t.Fatalf("expected storeRefreshMsg, got %T", rawMsg)
	}

	if len(msg.completedTasks) != 1 {
		t.Fatalf("completedTasks len = %d, want 1", len(msg.completedTasks))
	}
	if msg.completedTasks[0].ID != "TASK-1" {
		t.Errorf("completedTasks[0].ID = %q, want %q", msg.completedTasks[0].ID, "TASK-1")
	}
	if msg.completedTasks[0].Status != "success" {
		t.Errorf("Status = %q, want %q", msg.completedTasks[0].Status, "success")
	}
	if msg.metricsCard.TotalTasks != 1 {
		t.Errorf("TotalTasks = %d, want 1", msg.metricsCard.TotalTasks)
	}
	if msg.metricsCard.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", msg.metricsCard.Succeeded)
	}

	// Now delete the row and verify refresh picks up the change
	_, err = store.DB().Exec("DELETE FROM executions WHERE id = 'exec-1'")
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	cmd = storeRefreshCmd(store)
	rawMsg = cmd()
	msg = rawMsg.(storeRefreshMsg)

	if len(msg.completedTasks) != 0 {
		t.Errorf("after DELETE: completedTasks len = %d, want 0", len(msg.completedTasks))
	}
	if msg.metricsCard.TotalTasks != 0 {
		t.Errorf("after DELETE: TotalTasks = %d, want 0", msg.metricsCard.TotalTasks)
	}
}

func TestUpdateTokens_ModelAwareCost(t *testing.T) {
	m := NewModel("test")

	// Sonnet pricing: $3/M input, $15/M output
	// 1M input + 1M output = $3 + $15 = $18
	updated, _ := m.Update(updateTokensMsg{InputTokens: 1_000_000, OutputTokens: 1_000_000, TotalTokens: 2_000_000, Model: "claude-sonnet-4-6"})
	sonnetModel := updated.(Model)
	wantSonnet := memory.EstimateCost(1_000_000, 1_000_000, "claude-sonnet-4-6")
	if sonnetModel.metricsCard.TotalCostUSD != wantSonnet {
		t.Errorf("Sonnet cost = %.4f, want %.4f", sonnetModel.metricsCard.TotalCostUSD, wantSonnet)
	}

	// Opus pricing: $5/M input, $25/M output = $30 for same token count
	m2 := NewModel("test")
	updated2, _ := m2.Update(updateTokensMsg{InputTokens: 1_000_000, OutputTokens: 1_000_000, TotalTokens: 2_000_000, Model: "claude-opus-4-6"})
	opusModel := updated2.(Model)
	wantOpus := memory.EstimateCost(1_000_000, 1_000_000, "claude-opus-4-6")
	if opusModel.metricsCard.TotalCostUSD != wantOpus {
		t.Errorf("Opus cost = %.4f, want %.4f", opusModel.metricsCard.TotalCostUSD, wantOpus)
	}

	// Sonnet cost must be less than Opus cost for the same token count
	if wantSonnet >= wantOpus {
		t.Errorf("expected Sonnet ($%.4f) < Opus ($%.4f)", wantSonnet, wantOpus)
	}
}

func TestUpdateTokens_EmptyModelFallsBackToDefault(t *testing.T) {
	m := NewModel("test")
	updated, _ := m.Update(updateTokensMsg{InputTokens: 100_000, OutputTokens: 50_000, TotalTokens: 150_000, Model: ""})
	model := updated.(Model)
	want := memory.EstimateCost(100_000, 50_000, memory.DefaultModel)
	if model.metricsCard.TotalCostUSD != want {
		t.Errorf("cost with empty model = %.6f, want %.6f (DefaultModel=%s)", model.metricsCard.TotalCostUSD, want, memory.DefaultModel)
	}
}
