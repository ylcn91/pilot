package executor

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanEpicSuccess(t *testing.T) {
	tmpDir := t.TempDir()

	validOutput := `Here's the implementation plan:

1. **feat(db): set up database schema** - Create migration files for users and sessions tables
2. **feat(auth): implement auth service** - Build JWT-based authentication with refresh tokens
3. **feat(api): add API endpoints** - Create login, logout, and session management routes
4. **test(auth): write integration tests** - End-to-end tests for the auth flow`

	mockCmd := writeMockScript(t, tmpDir, validOutput, 0)

	runner := newTestRunner(mockCmd)
	task := &Task{
		ID:          "GH-100",
		Title:       "feat(auth): implement user authentication",
		Description: "Full auth system with JWT tokens and session management",
		ProjectPath: tmpDir,
	}

	plan, err := runner.PlanEpic(context.Background(), task, task.ProjectPath)
	if err != nil {
		t.Fatalf("PlanEpic returned unexpected error: %v", err)
	}

	if plan == nil {
		t.Fatal("PlanEpic returned nil plan")
	}

	if plan.ParentTask != task {
		t.Error("PlanEpic did not set ParentTask correctly")
	}

	if len(plan.Subtasks) != 4 {
		t.Fatalf("expected 4 subtasks, got %d", len(plan.Subtasks))
	}

	expectedTitles := []string{
		"feat(db): set up database schema",
		"feat(auth): implement auth service",
		"feat(api): add API endpoints",
		"test(auth): write integration tests",
	}

	for i, expected := range expectedTitles {
		if plan.Subtasks[i].Title != expected {
			t.Errorf("subtask %d: title = %q, want %q", i, plan.Subtasks[i].Title, expected)
		}
		if plan.Subtasks[i].Order != i+1 {
			t.Errorf("subtask %d: order = %d, want %d", i, plan.Subtasks[i].Order, i+1)
		}
		if plan.Subtasks[i].Description == "" {
			t.Errorf("subtask %d: description should not be empty", i)
		}
	}

	if plan.PlanOutput == "" {
		t.Error("PlanOutput should not be empty")
	}
}

func TestPlanEpicCLIFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Script exits with non-zero code (simulates CLI crash / API key missing / 500 error)
	scriptPath := filepath.Join(tmpDir, "mock-claude")
	script := "#!/bin/sh\necho 'Error: API key not set' >&2\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	runner := newTestRunner(scriptPath)
	task := &Task{
		ID:          "GH-101",
		Title:       "[epic] Build notification system",
		Description: "Multi-channel notifications",
		ProjectPath: tmpDir,
	}

	plan, err := runner.PlanEpic(context.Background(), task, task.ProjectPath)
	if err == nil {
		t.Fatal("PlanEpic should return error when CLI fails")
	}

	if plan != nil {
		t.Error("PlanEpic should return nil plan on CLI failure")
	}

	if !strings.Contains(err.Error(), "claude planning failed") {
		t.Errorf("error should mention claude planning failed, got: %v", err)
	}
}

func TestPlanEpicEmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Script succeeds but outputs nothing
	mockCmd := writeMockScript(t, tmpDir, "", 0)

	runner := newTestRunner(mockCmd)
	task := &Task{
		ID:          "GH-102",
		Title:       "[epic] Empty response task",
		Description: "Should fail on empty output",
		ProjectPath: tmpDir,
	}

	plan, err := runner.PlanEpic(context.Background(), task, task.ProjectPath)
	if err == nil {
		t.Fatal("PlanEpic should return error on empty output")
	}

	if plan != nil {
		t.Error("PlanEpic should return nil plan on empty output")
	}

	if !strings.Contains(err.Error(), "empty output") {
		t.Errorf("error should mention empty output, got: %v", err)
	}
}

func TestPlanEpicNoParseableSubtasks(t *testing.T) {
	tmpDir := t.TempDir()

	// Script outputs text but no numbered list — regex cannot parse subtasks
	unparseable := `I analyzed the task and here are my thoughts:

The system should handle authentication with multiple providers.
Consider using OAuth2 for social login integration.
Security is paramount for this implementation.`

	mockCmd := writeMockScript(t, tmpDir, unparseable, 0)

	runner := newTestRunner(mockCmd)
	task := &Task{
		ID:          "GH-103",
		Title:       "[epic] Unparseable planning output",
		Description: "Output with no numbered items triggers no-subtasks error",
		ProjectPath: tmpDir,
	}

	plan, err := runner.PlanEpic(context.Background(), task, task.ProjectPath)
	if err == nil {
		t.Fatal("PlanEpic should return error when no subtasks are parseable")
	}

	if plan != nil {
		t.Error("PlanEpic should return nil plan when regex finds nothing")
	}

	if !strings.Contains(err.Error(), "no subtasks found") {
		t.Errorf("error should mention no subtasks found, got: %v", err)
	}
}

func TestPlanEpicRegexParsesVariousFormats(t *testing.T) {
	// Validates that even when Claude returns different formatting,
	// the regex-based parseSubtasks still extracts subtasks correctly
	tmpDir := t.TempDir()

	tests := []struct {
		name           string
		output         string
		expectedCount  int
		expectedTitles []string
	}{
		{
			name: "step prefix format",
			output: `Here's the plan:
Step 1: Set up project scaffolding
Step 2: Implement core logic
Step 3: Add tests`,
			expectedCount:  3,
			expectedTitles: []string{"feat(fmt): set up project scaffolding", "feat(fmt): implement core logic", "feat(fmt): add tests"},
		},
		{
			name: "bold-wrapped numbers (GH-490 format)",
			output: `Analysis complete:

**1. chore(db): create database migration** - Schema changes for user tables
**2. feat(api): build API layer** - REST endpoints with validation
**3. feat(ui): add frontend components** - React forms and state management`,
			expectedCount:  3,
			expectedTitles: []string{"chore(db): create database migration", "feat(api): build API layer", "feat(ui): add frontend components"},
		},
		{
			name: "parenthesis format",
			output: `1) Initialize project
2) Add dependencies
3) Implement feature
4) Write tests`,
			expectedCount:  4,
			expectedTitles: []string{"feat(fmt): initialize project", "feat(fmt): add dependencies", "feat(fmt): implement feature", "feat(fmt): write tests"},
		},
		{
			name: "markdown heading format (GH-542)",
			output: `### 1. chore(db): create database migration - Schema changes
### 2. feat(api): build API layer - REST endpoints
### 3. feat(ui): add frontend components - React forms`,
			expectedCount:  3,
			expectedTitles: []string{"chore(db): create database migration", "feat(api): build API layer", "feat(ui): add frontend components"},
		},
		{
			name: "dash bullet format (GH-542)",
			output: `- **1. chore(db): add migration** - Schema changes
- **2. feat(api): build API** - REST endpoints
- **3. feat(ui): add frontend** - React forms`,
			expectedCount:  3,
			expectedTitles: []string{"chore(db): add migration", "feat(api): build API", "feat(ui): add frontend"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCmd := writeMockScript(t, tmpDir, tt.output, 0)
			runner := newTestRunner(mockCmd)
			task := &Task{
				ID:          "GH-FMT",
				Title:       "feat(fmt): test format parsing",
				Description: "Test regex parsing of different formats",
				ProjectPath: tmpDir,
			}

			plan, err := runner.PlanEpic(context.Background(), task, task.ProjectPath)
			if err != nil {
				t.Fatalf("PlanEpic failed: %v", err)
			}

			if len(plan.Subtasks) != tt.expectedCount {
				t.Fatalf("expected %d subtasks, got %d", tt.expectedCount, len(plan.Subtasks))
			}

			for i, expected := range tt.expectedTitles {
				if plan.Subtasks[i].Title != expected {
					t.Errorf("subtask %d: title = %q, want %q", i, plan.Subtasks[i].Title, expected)
				}
			}
		})
	}
}

func TestPlanEpicDefaultCommand(t *testing.T) {
	// When config is nil, PlanEpic defaults to "claude" command.
	// We verify by setting config with empty command — it should default to "claude".
	// Use a nonexistent binary to ensure it fails fast without hanging.
	runner := &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{
				Command: "nonexistent-claude-binary-for-test",
			},
		},
		running:           make(map[string]*exec.Cmd),
		progressCallbacks: make(map[string]ProgressCallback),
		tokenCallbacks:    make(map[string]TokenCallback),
		log:               slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		modelRouter:       NewModelRouter(nil, nil),
	}

	task := &Task{
		ID:          "GH-104",
		Title:       "[epic] Default command test",
		Description: "Should fail when binary is not found",
	}

	_, err := runner.PlanEpic(context.Background(), task, task.ProjectPath)
	if err == nil {
		t.Fatal("PlanEpic should fail when binary is not available")
	}

	if !strings.Contains(err.Error(), "claude planning failed") {
		t.Errorf("error should indicate claude planning failed, got: %v", err)
	}

	// Also verify nil config uses "claude" default
	runner2 := &Runner{
		config:            nil,
		running:           make(map[string]*exec.Cmd),
		progressCallbacks: make(map[string]ProgressCallback),
		tokenCallbacks:    make(map[string]TokenCallback),
		log:               slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		modelRouter:       NewModelRouter(nil, nil),
	}

	// Use a short timeout so it doesn't hang if "claude" binary exists
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = runner2.PlanEpic(ctx, task, task.ProjectPath)
	// We expect either an error (binary not found) or timeout (binary exists but hangs)
	if err == nil {
		t.Fatal("PlanEpic with nil config should still attempt to run")
	}
}

func TestPlanEpicContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	// Script that sleeps — will be cancelled
	scriptPath := filepath.Join(tmpDir, "mock-claude")
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	runner := newTestRunner(scriptPath)
	task := &Task{
		ID:          "GH-105",
		Title:       "[epic] Cancellation test",
		Description: "Should respect context cancellation",
		ProjectPath: tmpDir,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := runner.PlanEpic(ctx, task, task.ProjectPath)
	if err == nil {
		t.Fatal("PlanEpic should return error on cancelled context")
	}
}
