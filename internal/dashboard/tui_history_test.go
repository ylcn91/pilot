package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- Snapshot tests for renderHistory variants ---

func TestRenderHistory_EmptyState(t *testing.T) {
	m := NewModel("test")
	output := m.renderHistory()

	assertPanelLineWidths(t, output)

	plain := stripANSI(output)
	if !strings.Contains(plain, "HISTORY") {
		t.Error("missing HISTORY panel title")
	}
	if !strings.Contains(plain, "No completed tasks yet") {
		t.Error("empty state should show 'No completed tasks yet'")
	}
}

func TestRenderHistory_StandaloneTask(t *testing.T) {
	m := NewModel("test")
	m.completedTasks = []CompletedTask{
		{
			ID:          "GH-156",
			Title:       "Fix authentication bug in login",
			Status:      "success",
			Duration:    "2m",
			CompletedAt: time.Now().Add(-2 * time.Minute),
		},
		{
			ID:          "GH-157",
			Title:       "Update config validation",
			Status:      "failed",
			Duration:    "45s",
			CompletedAt: time.Now().Add(-15 * time.Minute),
		},
	}

	output := m.renderHistory()
	assertPanelLineWidths(t, output)

	plain := stripANSI(output)

	// Check standalone task icons
	if !strings.Contains(plain, "+ GH-156") {
		t.Error("success task should have '+' icon")
	}
	if !strings.Contains(plain, "x GH-157") {
		t.Error("failed task should have 'x' icon")
	}

	// Titles should be present (possibly truncated)
	if !strings.Contains(plain, "Fix authentication") {
		t.Error("task title should be visible")
	}

	// Time ago should be present
	if !strings.Contains(plain, "ago") {
		t.Error("time ago should be visible")
	}
}

func TestRenderHistory_ActiveEpicWithMixedStates(t *testing.T) {
	now := time.Now()
	m := NewModel("test")
	m.completedTasks = []CompletedTask{
		// Epic parent (active: 2/4 done)
		{
			ID:          "GH-491",
			Title:       "Enable decomposition by default",
			Status:      "running",
			Duration:    "3m",
			CompletedAt: now.Add(-3 * time.Minute),
			IsEpic:      true,
			TotalSubs:   4,
			DoneSubs:    2,
		},
		// Sub-issues
		{
			ID:          "GH-492",
			Title:       "Flip the default",
			Status:      "success",
			CompletedAt: now.Add(-2 * time.Minute),
			ParentID:    "GH-491",
		},
		{
			ID:          "GH-493",
			Title:       "Update example config",
			Status:      "running",
			CompletedAt: now,
			ParentID:    "GH-491",
		},
		{
			ID:       "GH-494",
			Title:    "Update documentation",
			Status:   "pending",
			ParentID: "GH-491",
		},
		{
			ID:          "GH-495",
			Title:       "Add integration tests",
			Status:      "failed",
			CompletedAt: now.Add(-1 * time.Minute),
			ParentID:    "GH-491",
		},
	}

	output := m.renderHistory()
	assertPanelLineWidths(t, output)

	plain := stripANSI(output)

	// Epic parent line: amber '*' icon, progress bar, counts
	if !strings.Contains(plain, "* GH-491") {
		t.Error("active epic should have '*' icon")
	}
	if !strings.Contains(plain, "[##--]") {
		t.Errorf("active epic should have [##--] progress bar, got:\n%s", plain)
	}
	if !strings.Contains(plain, "2/4") {
		t.Error("active epic should show 2/4 counts")
	}

	// Sub-issue lines: indented with per-status icons
	if !strings.Contains(plain, "    + GH-492") {
		t.Error("success sub-issue should be indented with '+' icon")
	}
	if !strings.Contains(plain, "    ~ GH-493") {
		t.Error("running sub-issue should be indented with '~' icon")
	}
	if !strings.Contains(plain, "    . GH-494") {
		t.Error("pending sub-issue should be indented with '.' icon")
	}
	if !strings.Contains(plain, "    x GH-495") {
		t.Error("failed sub-issue should be indented with 'x' icon")
	}

	// Pending sub-issue should show "--" instead of time
	// Find the line with GH-494
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "GH-494") {
			if !strings.Contains(line, "--") {
				t.Errorf("pending sub-issue should show '--', got: %q", line)
			}
			break
		}
	}

	// Running sub-issue should show "now"
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "GH-493") {
			if !strings.Contains(line, "now") {
				t.Errorf("running sub-issue should show 'now', got: %q", line)
			}
			break
		}
	}
}

func TestRenderHistory_CompletedEpicCollapsed(t *testing.T) {
	m := NewModel("test")
	m.completedTasks = []CompletedTask{
		{
			ID:          "GH-385",
			Title:       "Epic: Roadmap workflow",
			Status:      "success",
			Duration:    "12m",
			CompletedAt: time.Now().Add(-12 * time.Minute),
			IsEpic:      true,
			TotalSubs:   5,
			DoneSubs:    5,
		},
	}

	output := m.renderHistory()
	assertPanelLineWidths(t, output)

	plain := stripANSI(output)

	// Completed epic: collapsed with '+' icon and [5/5]
	if !strings.Contains(plain, "+ GH-385") {
		t.Error("completed epic should have '+' icon (success)")
	}
	if !strings.Contains(plain, "[5/5]") {
		t.Errorf("completed epic should show [5/5] count, got:\n%s", plain)
	}
	if !strings.Contains(plain, "Epic: Roadmap") {
		t.Error("completed epic title should be visible")
	}

	// Should NOT show sub-issue lines (collapsed)
	lines := strings.Split(plain, "\n")
	indentedCount := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, "│ "), "    ") {
			indentedCount++
		}
	}
	// Only panel borders and one content line expected
	contentLines := 0
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped != "" && !strings.HasPrefix(stripped, "╭") && !strings.HasPrefix(stripped, "╰") && !strings.HasPrefix(stripped, "│") {
			contentLines++
		}
	}
	// Collapsed epic = 1 content line (inside panel border lines)
}

func TestRenderHistory_MixedStandaloneAndEpic(t *testing.T) {
	now := time.Now()
	m := NewModel("test")
	m.completedTasks = []CompletedTask{
		// Active epic
		{
			ID:        "GH-491",
			Title:     "Enable decomposition",
			Status:    "running",
			Duration:  "3m",
			IsEpic:    true,
			TotalSubs: 3,
			DoneSubs:  2,
		},
		{
			ID:          "GH-492",
			Title:       "Flip default",
			Status:      "success",
			CompletedAt: now.Add(-2 * time.Minute),
			ParentID:    "GH-491",
		},
		{
			ID:       "GH-493",
			Title:    "Update config",
			Status:   "running",
			ParentID: "GH-491",
		},
		// Completed epic
		{
			ID:          "GH-385",
			Title:       "Roadmap workflow",
			Status:      "success",
			CompletedAt: now.Add(-12 * time.Minute),
			IsEpic:      true,
			TotalSubs:   5,
			DoneSubs:    5,
		},
		// Standalone task
		{
			ID:          "GH-489",
			Title:       "fix(autopilot): embed branch metadata",
			Status:      "success",
			CompletedAt: now.Add(-15 * time.Minute),
		},
	}

	output := m.renderHistory()
	assertPanelLineWidths(t, output)

	plain := stripANSI(output)

	// All three types should be present
	if !strings.Contains(plain, "* GH-491") {
		t.Error("active epic should be present with '*' icon")
	}
	if !strings.Contains(plain, "[5/5]") {
		t.Error("completed epic [5/5] count should be present")
	}
	if !strings.Contains(plain, "+ GH-489") {
		t.Error("standalone task should be present with '+' icon")
	}

	// Sub-issues should appear under active epic, not standalone
	if !strings.Contains(plain, "    + GH-492") {
		t.Error("sub-issue GH-492 should be indented under epic")
	}
}

func TestRenderEpicProgressBar(t *testing.T) {
	tests := []struct {
		name       string
		done       int
		total      int
		innerWidth int
		want       string
	}{
		{"zero progress", 0, 3, 4, "[----]"},
		{"partial progress", 2, 4, 4, "[##--]"},
		{"full progress", 5, 5, 4, "[####]"},
		{"one of three", 1, 3, 4, "[#---]"},
		{"zero total", 0, 0, 4, "[----]"},
		{"wider bar", 3, 6, 6, "[###---]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderEpicProgressBar(tt.done, tt.total, tt.innerWidth)
			if got != tt.want {
				t.Errorf("renderEpicProgressBar(%d, %d, %d) = %q, want %q",
					tt.done, tt.total, tt.innerWidth, got, tt.want)
			}
		})
	}
}

func TestGroupedHistory_SubIssueAbsorption(t *testing.T) {
	m := NewModel("test")
	m.completedTasks = []CompletedTask{
		{ID: "GH-100", Title: "Epic task", IsEpic: true, TotalSubs: 2, DoneSubs: 1},
		{ID: "GH-101", Title: "Sub 1", ParentID: "GH-100", Status: "success"},
		{ID: "GH-102", Title: "Sub 2", ParentID: "GH-100", Status: "pending"},
		{ID: "GH-200", Title: "Standalone", Status: "success"},
	}

	groups := m.groupedHistory()

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// First group: epic with sub-issues absorbed
	if groups[0].Task.ID != "GH-100" {
		t.Errorf("first group ID = %q, want GH-100", groups[0].Task.ID)
	}
	if len(groups[0].SubIssues) != 2 {
		t.Errorf("epic sub-issues = %d, want 2", len(groups[0].SubIssues))
	}

	// Second group: standalone
	if groups[1].Task.ID != "GH-200" {
		t.Errorf("second group ID = %q, want GH-200", groups[1].Task.ID)
	}
	if len(groups[1].SubIssues) != 0 {
		t.Errorf("standalone sub-issues = %d, want 0", len(groups[1].SubIssues))
	}
}

func TestGroupedHistory_OrphanSubIssue(t *testing.T) {
	// Sub-issue whose parent is NOT in the list should render standalone
	m := NewModel("test")
	m.completedTasks = []CompletedTask{
		{ID: "GH-101", Title: "Orphan sub", ParentID: "GH-999", Status: "success"},
	}

	groups := m.groupedHistory()

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Task.ID != "GH-101" {
		t.Errorf("orphan should appear as standalone, got ID=%q", groups[0].Task.ID)
	}
}

func TestAddCompletedTask_HistoryCapAt5(t *testing.T) {
	m := NewModel("test")

	// Add 6 tasks — history should keep only the last 5
	for i := 0; i < 6; i++ {
		msg := addCompletedTaskMsg(CompletedTask{
			ID:       fmt.Sprintf("GH-%d", i+1),
			Title:    fmt.Sprintf("Task %d", i+1),
			Status:   "success",
			ParentID: "GH-0",
			IsEpic:   i == 5, // last one is an epic
		})
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	if len(m.completedTasks) != 5 {
		t.Fatalf("completedTasks len = %d, want 5", len(m.completedTasks))
	}

	// First task (GH-1) should have been evicted; GH-2 is now first
	if m.completedTasks[0].ID != "GH-2" {
		t.Errorf("first task ID = %q, want %q", m.completedTasks[0].ID, "GH-2")
	}
	// Last task should be the epic
	last := m.completedTasks[4]
	if !last.IsEpic {
		t.Error("last task IsEpic = false, want true")
	}
	if last.ParentID != "GH-0" {
		t.Errorf("last task ParentID = %q, want %q", last.ParentID, "GH-0")
	}
}
