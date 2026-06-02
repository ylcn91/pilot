package dashboard

import (
	"strings"
	"testing"
	"time"
)

// TestModelUpdate_GToggle verifies 'g' key toggles graph on/off.
func TestModelUpdate_GToggle(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphHidden
	m.projectPath = "."

	// Hidden → Visible
	updated, _ := m.Update(makeKey("g"))
	m = updated.(Model)
	if m.gitGraphMode != GitGraphVisible {
		t.Errorf("after 1st g: mode = %d, want GitGraphVisible(%d)", m.gitGraphMode, GitGraphVisible)
	}

	// Visible → Hidden
	updated, _ = m.Update(makeKey("g"))
	m = updated.(Model)
	if m.gitGraphMode != GitGraphHidden {
		t.Errorf("after 2nd g: mode = %d, want GitGraphHidden(%d)", m.gitGraphMode, GitGraphHidden)
	}
}

// TestModelUpdate_TabFocus verifies Tab toggles focus when graph is visible.
func TestModelUpdate_TabFocus(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.gitGraphFocus = false

	updated, _ := m.Update(makeKey("tab"))
	m = updated.(Model)
	if !m.gitGraphFocus {
		t.Error("Tab should set gitGraphFocus=true when graph is visible")
	}

	updated, _ = m.Update(makeKey("tab"))
	m = updated.(Model)
	if m.gitGraphFocus {
		t.Error("second Tab should set gitGraphFocus=false")
	}
}

// TestModelUpdate_TabNoFocusWhenHidden verifies Tab is a no-op when graph hidden.
func TestModelUpdate_TabNoFocusWhenHidden(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphHidden
	m.gitGraphFocus = false

	updated, _ := m.Update(makeKey("tab"))
	m = updated.(Model)
	if m.gitGraphFocus {
		t.Error("Tab should NOT toggle focus when graph is hidden")
	}
}

// TestModelUpdate_ScrollWhenFocused verifies j/k scroll the graph when focused.
func TestModelUpdate_ScrollWhenFocused(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.gitGraphFocus = true
	m.gitGraphScroll = 5
	m.height = 40 // viewport = 35
	m.gitGraphState = &GitGraphState{
		Lines: make([]GitGraphLine, 50),
	}

	// 'j' scrolls down
	updated, _ := m.Update(makeKey("j"))
	m = updated.(Model)
	if m.gitGraphScroll != 6 {
		t.Errorf("after j: scroll = %d, want 6", m.gitGraphScroll)
	}

	// 'k' scrolls up
	updated, _ = m.Update(makeKey("k"))
	m = updated.(Model)
	if m.gitGraphScroll != 5 {
		t.Errorf("after k: scroll = %d, want 5", m.gitGraphScroll)
	}
}

// TestModelUpdate_ScrollBoundaries verifies scroll doesn't go out of bounds.
func TestModelUpdate_ScrollBoundaries(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.gitGraphFocus = true
	m.gitGraphScroll = 0
	m.height = 8 // viewport = 3
	m.gitGraphState = &GitGraphState{
		Lines: make([]GitGraphLine, 5),
	}

	// Can't scroll up past 0
	updated, _ := m.Update(makeKey("k"))
	m = updated.(Model)
	if m.gitGraphScroll != 0 {
		t.Errorf("scroll should stay at 0, got %d", m.gitGraphScroll)
	}

	// maxScroll = 5 - 3 = 2; can't scroll past that
	m.gitGraphScroll = 2
	updated, _ = m.Update(makeKey("j"))
	m = updated.(Model)
	if m.gitGraphScroll != 2 {
		t.Errorf("scroll should stay at 2 (max), got %d", m.gitGraphScroll)
	}
}

// TestModelUpdate_DashboardScrollWhenNotFocused verifies j/k select tasks when not focused.
func TestModelUpdate_DashboardScrollWhenNotFocused(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.gitGraphFocus = false
	m.tasks = []TaskDisplay{
		{ID: "1", Title: "Task A", Status: "running"},
		{ID: "2", Title: "Task B", Status: "queued"},
	}
	m.selectedTask = 0

	updated, _ := m.Update(makeKey("j"))
	m = updated.(Model)
	if m.selectedTask != 1 {
		t.Errorf("j should move selectedTask to 1, got %d", m.selectedTask)
	}
	if m.gitGraphScroll != 0 {
		t.Errorf("gitGraphScroll should stay at 0, got %d", m.gitGraphScroll)
	}
}

// TestModelUpdate_HalfPageScroll verifies Ctrl+D/Ctrl+U half-page scrolling.
func TestModelUpdate_HalfPageScroll(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.gitGraphFocus = true
	m.gitGraphScroll = 0
	m.height = 40 // viewport = 35, half-page = 17
	m.gitGraphState = &GitGraphState{
		Lines: make([]GitGraphLine, 100),
	}

	// Ctrl+D: down half-page (35/2 = 17)
	updated, _ := m.Update(makeKey("ctrl+d"))
	m = updated.(Model)
	if m.gitGraphScroll != 17 {
		t.Errorf("ctrl+d: scroll = %d, want 17", m.gitGraphScroll)
	}

	// Ctrl+U: up half-page (back to 0)
	updated, _ = m.Update(makeKey("ctrl+u"))
	m = updated.(Model)
	if m.gitGraphScroll != 0 {
		t.Errorf("ctrl+u: scroll = %d, want 0", m.gitGraphScroll)
	}
}

// TestModelUpdate_GitRefreshMsg verifies state is updated on refresh.
func TestModelUpdate_GitRefreshMsg(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible

	state := &GitGraphState{
		TotalCount:  42,
		LastRefresh: time.Now(),
		Lines: []GitGraphLine{
			{SHA: "abc1234", Message: "test commit"},
		},
	}

	updated, _ := m.Update(gitRefreshMsg{state: state})
	m = updated.(Model)

	if m.gitGraphState == nil {
		t.Fatal("gitGraphState should be set after gitRefreshMsg")
	}
	if m.gitGraphState.TotalCount != 42 {
		t.Errorf("TotalCount = %d, want 42", m.gitGraphState.TotalCount)
	}
}

// TestModelUpdate_GitRefreshTickHidden verifies no refresh cmd when graph is hidden.
func TestModelUpdate_GitRefreshTickHidden(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphHidden

	_, cmd := m.Update(gitRefreshTickMsg{})
	if cmd != nil {
		t.Error("gitRefreshTickMsg when hidden should return nil cmd (no refresh)")
	}
}

// TestViewWithGitGraph_SideBySide verifies View renders both panels side-by-side.
func TestViewWithGitGraph_SideBySide(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.width = 140 // graphWidth = 140 - 69 - 2 = 69 → full mode (>= 65)
	m.height = 40
	m.gitGraphState = &GitGraphState{
		TotalCount: 2,
		Lines: []GitGraphLine{
			{GraphChars: "● ", SHA: "7eb8da1", Author: "Alice", Message: "initial commit"},
		},
	}

	output := m.View()
	if output == "" {
		t.Error("View() returned empty string with git graph visible")
	}

	plain := stripANSI(output)

	if !strings.Contains(plain, "PILOT") {
		t.Error("View() should contain dashboard header")
	}
	if !strings.Contains(plain, "GIT GRAPH") {
		t.Error("View() should contain GIT GRAPH panel")
	}
}

// TestViewHidden_NoGraph verifies View renders normally when graph is hidden.
func TestViewHidden_NoGraph(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphHidden
	m.width = 120
	m.height = 40

	output := m.View()
	plain := stripANSI(output)

	if strings.Contains(plain, "GIT GRAPH") {
		t.Error("View() should NOT contain GIT GRAPH when hidden")
	}
	if !strings.Contains(plain, "PILOT") {
		t.Error("View() should contain dashboard header")
	}
}

// TestSetProjectPath verifies SetProjectPath sets the field.
func TestSetProjectPath(t *testing.T) {
	m := NewModel("test")
	m.SetProjectPath("/tmp/myrepo")
	if m.projectPath != "/tmp/myrepo" {
		t.Errorf("projectPath = %q, want %q", m.projectPath, "/tmp/myrepo")
	}
}
