package dashboard

import (
	"strings"
	"testing"
)

// =============================================================================
// GH-2167: Git graph follows focused task's project
// =============================================================================

// TestSyncGitGraph_SwitchesProjectOnTaskChange verifies that navigating to a task
// with a different project path triggers a git graph refresh.
func TestSyncGitGraph_SwitchesProjectOnTaskChange(t *testing.T) {
	m := NewModel("test")
	m.SetProjectPath("/home/user/pilot")
	m.gitGraphMode = GitGraphVisible
	m.tasks = []TaskDisplay{
		{ID: "1", Title: "Task A", Status: "running", ProjectPath: "/home/user/pilot", ProjectName: "pilot"},
		{ID: "2", Title: "Task B", Status: "queued", ProjectPath: "/home/user/aso-generator", ProjectName: "aso-generator"},
	}
	m.selectedTask = 0

	// Navigate down to task B (different project)
	updated, cmd := m.Update(makeKey("j"))
	m = updated.(Model)

	if m.selectedTask != 1 {
		t.Errorf("selectedTask = %d, want 1", m.selectedTask)
	}
	if m.projectPath != "/home/user/aso-generator" {
		t.Errorf("projectPath = %q, want /home/user/aso-generator", m.projectPath)
	}
	if m.gitProjectName != "aso-generator" {
		t.Errorf("gitProjectName = %q, want aso-generator", m.gitProjectName)
	}
	if cmd == nil {
		t.Error("expected refresh cmd when project changes, got nil")
	}
	if m.gitGraphScroll != 0 {
		t.Errorf("gitGraphScroll should reset to 0, got %d", m.gitGraphScroll)
	}
}

// TestSyncGitGraph_NoRefreshWhenSameProject verifies no refresh when navigating
// between tasks in the same project.
func TestSyncGitGraph_NoRefreshWhenSameProject(t *testing.T) {
	m := NewModel("test")
	m.SetProjectPath("/home/user/pilot")
	m.gitGraphMode = GitGraphVisible
	m.tasks = []TaskDisplay{
		{ID: "1", Title: "Task A", Status: "running", ProjectPath: "/home/user/pilot", ProjectName: "pilot"},
		{ID: "2", Title: "Task B", Status: "queued", ProjectPath: "/home/user/pilot", ProjectName: "pilot"},
	}
	m.selectedTask = 0

	updated, cmd := m.Update(makeKey("j"))
	m = updated.(Model)

	if m.selectedTask != 1 {
		t.Errorf("selectedTask = %d, want 1", m.selectedTask)
	}
	// No refresh needed — same project
	if cmd != nil {
		t.Error("expected nil cmd when project unchanged")
	}
}

// TestSyncGitGraph_FallsBackToDefault verifies that when tasks have no project path,
// the default project path is used.
func TestSyncGitGraph_FallsBackToDefault(t *testing.T) {
	m := NewModel("test")
	m.SetProjectPath("/home/user/pilot")
	m.gitGraphMode = GitGraphVisible
	m.projectPath = "/home/user/aso-generator" // currently showing different project
	m.tasks = []TaskDisplay{
		{ID: "1", Title: "Task A", Status: "running"}, // no ProjectPath
	}
	m.selectedTask = 0

	cmd := m.syncGitGraphToSelectedTask()

	if m.projectPath != "/home/user/pilot" {
		t.Errorf("projectPath = %q, want default /home/user/pilot", m.projectPath)
	}
	if cmd == nil {
		t.Error("expected refresh cmd when reverting to default project")
	}
}

// TestGitGraphTitle_IncludesProjectName verifies the panel title shows the project name.
func TestGitGraphTitle_IncludesProjectName(t *testing.T) {
	m := NewModel("test")
	m.gitGraphMode = GitGraphVisible
	m.gitProjectName = "aso-generator"
	m.width = 140
	m.height = 40
	m.gitGraphState = &GitGraphState{
		TotalCount: 1,
		Lines: []GitGraphLine{
			{GraphChars: "● ", SHA: "abc1234", Message: "init"},
		},
	}

	output := m.renderGitGraph()
	plain := stripANSI(output)

	if !strings.Contains(plain, "ASO-GENERATOR") {
		t.Errorf("git graph title should contain project name (uppercased), got:\n%s", plain)
	}
}

// TestSyncGitGraph_UpNavigation verifies project switch on upward navigation.
func TestSyncGitGraph_UpNavigation(t *testing.T) {
	m := NewModel("test")
	m.SetProjectPath("/home/user/pilot")
	m.gitGraphMode = GitGraphVisible
	m.tasks = []TaskDisplay{
		{ID: "1", Title: "Task A", Status: "running", ProjectPath: "/home/user/pilot", ProjectName: "pilot"},
		{ID: "2", Title: "Task B", Status: "queued", ProjectPath: "/home/user/aso-generator", ProjectName: "aso-generator"},
	}
	m.selectedTask = 1
	m.projectPath = "/home/user/aso-generator"
	m.gitProjectName = "aso-generator"

	updated, cmd := m.Update(makeKey("k"))
	m = updated.(Model)

	if m.selectedTask != 0 {
		t.Errorf("selectedTask = %d, want 0", m.selectedTask)
	}
	if m.projectPath != "/home/user/pilot" {
		t.Errorf("projectPath = %q, want /home/user/pilot", m.projectPath)
	}
	if cmd == nil {
		t.Error("expected refresh cmd when project changes via k")
	}
}
