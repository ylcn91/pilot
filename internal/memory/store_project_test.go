package memory

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProjectCRUD(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	project := &Project{
		Path:             "/path/to/project",
		Name:             "my-project",
		NavigatorEnabled: true,
		LastActive:       time.Now(),
		Settings:         map[string]interface{}{"theme": "dark"},
	}

	if err := store.SaveProject(project); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}

	retrieved, err := store.GetProject("/path/to/project")
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}

	if retrieved.Name != "my-project" {
		t.Errorf("Expected name 'my-project', got '%s'", retrieved.Name)
	}
	if !retrieved.NavigatorEnabled {
		t.Error("Expected NavigatorEnabled to be true")
	}
}

func TestGetAllProjects(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	_ = store.SaveProject(&Project{Path: "/path/1", Name: "project-1"})
	_ = store.SaveProject(&Project{Path: "/path/2", Name: "project-2"})

	projects, err := store.GetAllProjects()
	if err != nil {
		t.Fatalf("GetAllProjects failed: %v", err)
	}

	if len(projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(projects))
	}
}

func TestGetProject_InvalidSettingsJSON(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Insert project with invalid JSON settings directly into DB
	_, err := store.db.Exec(`
		INSERT INTO projects (path, name, navigator_enabled, settings)
		VALUES (?, ?, ?, ?)
	`, "/test/project", "test", true, "invalid-json{{{")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Capture slog output
	var buf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(oldLogger)

	// Should not return error, but should log warning
	project, err := store.GetProject("/test/project")
	if err != nil {
		t.Errorf("GetProject should not error on invalid settings JSON: %v", err)
	}
	if project == nil {
		t.Fatal("project should not be nil")
	}
	if project.Settings != nil {
		t.Errorf("Settings should be nil after unmarshal failure, got %v", project.Settings)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to unmarshal project settings") {
		t.Errorf("expected warning log about unmarshal failure, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "/test/project") {
		t.Errorf("expected project path in log, got: %s", logOutput)
	}
}

func TestGetAllProjects_InvalidSettingsJSON(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Insert valid and invalid projects
	_, _ = store.db.Exec(`INSERT INTO projects (path, name, navigator_enabled, settings) VALUES (?, ?, ?, ?)`,
		"/valid/project", "valid", true, `{"theme":"dark"}`)
	_, _ = store.db.Exec(`INSERT INTO projects (path, name, navigator_enabled, settings) VALUES (?, ?, ?, ?)`,
		"/invalid/project", "invalid", true, "not-valid-json")

	var buf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(oldLogger)

	projects, err := store.GetAllProjects()
	if err != nil {
		t.Errorf("GetAllProjects should not error: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to unmarshal project settings") {
		t.Errorf("expected warning log, got: %s", logOutput)
	}
}

func TestProjectSettings(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create project with complex settings
	settings := map[string]interface{}{
		"theme":        "dark",
		"autoCommit":   true,
		"maxTokens":    100000,
		"excludePaths": []interface{}{"/vendor", "/node_modules"},
	}

	project := &Project{
		Path:             "/path/to/project",
		Name:             "test-project",
		NavigatorEnabled: true,
		LastActive:       time.Now(),
		Settings:         settings,
	}

	if err := store.SaveProject(project); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}

	retrieved, err := store.GetProject("/path/to/project")
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}

	if retrieved.Settings["theme"] != "dark" {
		t.Errorf("Settings[theme] = %v, want 'dark'", retrieved.Settings["theme"])
	}
	if retrieved.Settings["autoCommit"] != true {
		t.Errorf("Settings[autoCommit] = %v, want true", retrieved.Settings["autoCommit"])
	}
}

func TestGetProject_NotFound(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	_, err := store.GetProject("/nonexistent/path")
	if err == nil {
		t.Error("GetProject should return error for nonexistent project")
	}
}
