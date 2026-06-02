package telegram

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/comms"
)

// TestGetActiveProjectPath tests active project path retrieval
func TestGetActiveProjectPath(t *testing.T) {
	ch := newTestCommsHandler()
	h := &Handler{
		projectPath:  "/default/path",
		commsHandler: ch,
	}

	// Test default path
	path := h.getActiveProjectPath("chat1")
	if path != "/default/path" {
		t.Errorf("getActiveProjectPath() = %q, want %q", path, "/default/path")
	}

	// Other chat still gets default
	path = h.getActiveProjectPath("chat2")
	if path != "/default/path" {
		t.Errorf("getActiveProjectPath() = %q, want %q", path, "/default/path")
	}
}

// TestSetActiveProject tests project switching
func TestSetActiveProject(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "project-a", Path: "/path/a"},
			{Name: "project-b", Path: "/path/b"},
		},
	}

	ch := comms.NewHandler(&comms.HandlerConfig{
		Messenger:    &noopMessenger{},
		Projects:     projects,
		TaskIDPrefix: "TG",
	})
	h := &Handler{
		projects:     projects,
		commsHandler: ch,
	}

	tests := []struct {
		name        string
		chatID      string
		projectName string
		wantPath    string
		wantErr     bool
	}{
		{
			name:        "switch to existing project",
			chatID:      "chat1",
			projectName: "project-a",
			wantPath:    "/path/a",
			wantErr:     false,
		},
		{
			name:        "switch to another project",
			chatID:      "chat1",
			projectName: "project-b",
			wantPath:    "/path/b",
			wantErr:     false,
		},
		{
			name:        "non-existent project",
			chatID:      "chat1",
			projectName: "unknown",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, err := h.setActiveProject(tt.chatID, tt.projectName)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if proj.Path != tt.wantPath {
				t.Errorf("project path = %q, want %q", proj.Path, tt.wantPath)
			}
		})
	}
}

// TestSetActiveProjectNoProjects tests error when no projects configured
func TestSetActiveProjectNoProjects(t *testing.T) {
	h := &Handler{
		projects:     nil,
		commsHandler: newTestCommsHandler(),
	}

	_, err := h.setActiveProject("chat1", "any")
	if err == nil {
		t.Error("expected error when projects is nil")
	}
}

// TestGetActiveProjectInfo tests project info retrieval
func TestGetActiveProjectInfo(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "test", Path: "/test/path"},
		},
	}

	tests := []struct {
		name     string
		projects comms.ProjectSource
		wantNil  bool
	}{
		{
			name:     "with projects",
			projects: projects,
			wantNil:  false,
		},
		{
			name:     "nil projects",
			projects: nil,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				projects:     tt.projects,
				projectPath:  "/test/path",
				commsHandler: newTestCommsHandler(),
			}

			got := h.getActiveProjectInfo("chat1")

			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
			} else {
				if got == nil {
					t.Error("expected non-nil ProjectInfo")
				}
			}
		})
	}
}

// TestProjectInfo tests ProjectInfo struct
func TestProjectInfo(t *testing.T) {
	proj := &comms.ProjectInfo{
		Name:          "test-project",
		Path:          "/path/to/project",
		Navigator:     true,
		DefaultBranch: "main",
	}

	if proj.Name != "test-project" {
		t.Errorf("Name = %q, want test-project", proj.Name)
	}
	if proj.Path != "/path/to/project" {
		t.Errorf("Path = %q, want /path/to/project", proj.Path)
	}
	if !proj.Navigator {
		t.Error("Navigator should be true")
	}
	if proj.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main", proj.DefaultBranch)
	}
}

// TestMockProjectSourceMethods tests all MockProjectSource methods
func TestMockProjectSourceMethods(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "alpha", Path: "/alpha"},
			{Name: "beta", Path: "/beta"},
		},
	}

	// GetProjectByName
	alpha := projects.GetProjectByName("alpha")
	if alpha == nil || alpha.Path != "/alpha" {
		t.Error("GetProjectByName(alpha) failed")
	}

	notFound := projects.GetProjectByName("notfound")
	if notFound != nil {
		t.Error("GetProjectByName(notfound) should return nil")
	}

	// GetProjectByPath
	beta := projects.GetProjectByPath("/beta")
	if beta == nil || beta.Name != "beta" {
		t.Error("GetProjectByPath(/beta) failed")
	}

	notFoundPath := projects.GetProjectByPath("/notfound")
	if notFoundPath != nil {
		t.Error("GetProjectByPath(/notfound) should return nil")
	}

	// GetDefaultProject
	def := projects.GetDefaultProject()
	if def == nil || def.Name != "alpha" {
		t.Error("GetDefaultProject should return first project")
	}

	// ListProjects
	list := projects.ListProjects()
	if len(list) != 2 {
		t.Errorf("ListProjects len = %d, want 2", len(list))
	}
}

// TestMockProjectSourceEmpty tests empty project source
func TestMockProjectSourceEmpty(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{},
	}

	if projects.GetDefaultProject() != nil {
		t.Error("GetDefaultProject should return nil for empty source")
	}
	if len(projects.ListProjects()) != 0 {
		t.Error("ListProjects should return empty list")
	}
}

// TestActiveProjectUsedInTaskPaths verifies that after switching projects,
// all task-related functions use the active project path, not the default.
// Regression test for GH-1685.
func TestActiveProjectUsedInTaskPaths(t *testing.T) {
	defaultPath := "/default/project"
	activePath := "/active/project"
	chatID := "chat123"

	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "active-proj", Path: activePath},
		},
	}
	ch := comms.NewHandler(&comms.HandlerConfig{
		Messenger:    &noopMessenger{},
		Projects:     projects,
		ProjectPath:  defaultPath,
		TaskIDPrefix: "TG",
	})

	h := &Handler{
		projectPath:  defaultPath,
		projects:     projects,
		commsHandler: ch,
	}

	// Before switching, should return default
	if got := h.getActiveProjectPath(chatID); got != defaultPath {
		t.Errorf("before switch: getActiveProjectPath() = %q, want %q", got, defaultPath)
	}

	// Switch project for this chat via commsHandler
	if err := ch.SetActiveProject(chatID, "active-proj"); err != nil {
		t.Fatal(err)
	}

	// After switching, should return active project path
	if got := h.getActiveProjectPath(chatID); got != activePath {
		t.Errorf("after switch: getActiveProjectPath() = %q, want %q", got, activePath)
	}

	// Verify confirmation message uses active path
	confirmMsg := FormatTaskConfirmation("TEST-01", "test task", h.getActiveProjectPath(chatID))
	if !strings.Contains(confirmMsg, activePath) {
		t.Errorf("confirmation message should contain active path %q, got:\n%s", activePath, confirmMsg)
	}
	if strings.Contains(confirmMsg, defaultPath) {
		t.Errorf("confirmation message should NOT contain default path %q, got:\n%s", defaultPath, confirmMsg)
	}

	// Other chat should still get default
	if got := h.getActiveProjectPath("other-chat"); got != defaultPath {
		t.Errorf("other chat: getActiveProjectPath() = %q, want %q", got, defaultPath)
	}
}
