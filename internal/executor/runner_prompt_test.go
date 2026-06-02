package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPrompt(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TASK-123",
		Title:       "Add authentication",
		Description: "Implement user authentication flow",
		ProjectPath: "/path/to/project",
		Branch:      "pilot/TASK-123",
	}

	prompt := runner.BuildPrompt(task, task.ProjectPath)

	if prompt == "" {
		t.Error("buildPrompt returned empty string")
	}

	// Check that key elements are in the prompt
	tests := []string{
		"TASK-123",
		"Implement user authentication flow",
		"pilot/TASK-123",
		"Commit",
	}

	for _, expected := range tests {
		if !contains(prompt, expected) {
			t.Errorf("Prompt missing expected content: %s", expected)
		}
	}
}

func TestBuildPromptNoBranch(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TASK-456",
		Description: "Fix a bug",
		ProjectPath: "/path/to/project",
		Branch:      "", // No branch
	}

	prompt := runner.BuildPrompt(task, task.ProjectPath)

	if !contains(prompt, "current branch") {
		t.Error("Prompt should mention current branch when Branch is empty")
	}
	if contains(prompt, "Create a new git branch") {
		t.Error("Prompt should not mention creating branch when Branch is empty")
	}
}

func TestBuildPromptImageTask(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "IMG-1",
		Description: "What is shown in this image?",
		ProjectPath: "/path/to/project",
		ImagePath:   "/path/to/screenshot.png",
	}

	prompt := runner.BuildPrompt(task, task.ProjectPath)

	if !contains(prompt, "/path/to/screenshot.png") {
		t.Error("Image task prompt should contain image path")
	}
	if !contains(prompt, "What is shown in this image?") {
		t.Error("Image task prompt should contain description")
	}
	if contains(prompt, "Navigator") {
		t.Error("Image task should not include Navigator workflow")
	}
}

func TestBuildPromptSkipsNavigatorForTrivialTasks(t *testing.T) {
	// Create a temp directory with .agent/ to simulate Navigator project
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	runner := NewRunner()

	tests := []struct {
		name            string
		description     string
		expectNavigator bool
	}{
		{
			name:            "trivial task - fix typo",
			description:     "Fix typo in README",
			expectNavigator: false,
		},
		{
			name:            "trivial task - add logging",
			description:     "Add log statement to debug function",
			expectNavigator: false,
		},
		{
			name:            "trivial task - update comment",
			description:     "Update comment in handler.go",
			expectNavigator: false,
		},
		{
			name:            "trivial task - rename variable",
			description:     "Rename variable from foo to bar",
			expectNavigator: false,
		},
		{
			name:            "medium task - add feature",
			description:     "Add user authentication with JWT tokens and session management",
			expectNavigator: true,
		},
		{
			name:            "complex task - refactor",
			description:     "Refactor the authentication module to use OAuth2",
			expectNavigator: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{
				ID:          "TEST-1",
				Title:       tc.name,
				Description: tc.description,
				ProjectPath: tmpDir,
				Branch:      "test-branch",
			}

			prompt := runner.BuildPrompt(task, task.ProjectPath)

			hasNavigator := contains(prompt, "## Autonomous Execution Workflow")
			hasTrivialMarker := contains(prompt, "trivial change")

			if tc.expectNavigator {
				if !hasNavigator {
					t.Errorf("Expected Navigator session for non-trivial task, got prompt: %s", prompt)
				}
				if hasTrivialMarker {
					t.Errorf("Non-trivial task should not have trivial marker")
				}
			} else {
				if hasNavigator {
					t.Errorf("Trivial task should skip Navigator, got prompt: %s", prompt)
				}
				if !hasTrivialMarker {
					t.Errorf("Trivial task should have trivial marker, got prompt: %s", prompt)
				}
			}
		})
	}
}

// Test self-review prompt generation (GH-364)
func TestBuildSelfReviewPrompt(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TEST-001",
		Title:       "Test task",
		Description: "Test description",
		ProjectPath: "/tmp/test",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	// Verify key elements
	if !strings.Contains(prompt, "Self-Review Phase") {
		t.Error("Prompt should contain Self-Review Phase header")
	}
	if !strings.Contains(prompt, "git diff") {
		t.Error("Prompt should include diff analysis")
	}
	if !strings.Contains(prompt, "go build") {
		t.Error("Prompt should include build verification")
	}
	if !strings.Contains(prompt, "REVIEW_PASSED") {
		t.Error("Prompt should include success signal")
	}
	if !strings.Contains(prompt, "REVIEW_FIXED") {
		t.Error("Prompt should include fixed signal")
	}
	if !strings.Contains(prompt, "Wiring Check") {
		t.Error("Prompt should include wiring check")
	}
	if !strings.Contains(prompt, "Method Existence Check") {
		t.Error("Prompt should include method existence check")
	}
}

// Test self-review config option (GH-364)
func TestBackendConfig_SkipSelfReview(t *testing.T) {
	// Default config should have SkipSelfReview as false
	config := DefaultBackendConfig()
	if config.SkipSelfReview {
		t.Error("Default config should not skip self-review")
	}

	// Test with skip enabled
	config.SkipSelfReview = true
	if !config.SkipSelfReview {
		t.Error("SkipSelfReview should be true when set")
	}
}

// Test that self-review is skipped for trivial tasks (GH-364)
func TestSelfReviewSkipsTrivialTasks(t *testing.T) {
	// Trivial task should be skipped
	trivialTask := &Task{
		ID:          "TRIV-001",
		Title:       "Fix typo in README",
		Description: "Fix typo in README",
		ProjectPath: "/tmp/test",
	}

	complexity := DetectComplexity(trivialTask)
	if !complexity.ShouldSkipNavigator() {
		t.Error("Expected trivial task to be detected as trivial")
	}

	// The self-review should skip trivial tasks without needing backend execution
	// We can't fully test runSelfReview without mocking, but we verify the complexity check
	if complexity != ComplexityTrivial {
		t.Errorf("Expected complexity to be trivial, got %s", complexity)
	}

	// Non-trivial task should NOT be skipped
	mediumTask := &Task{
		ID:          "MED-001",
		Title:       "Add user authentication with JWT tokens",
		Description: "Implement user authentication flow with JWT tokens and session management",
		ProjectPath: "/tmp/test",
	}

	mediumComplexity := DetectComplexity(mediumTask)
	if mediumComplexity.ShouldSkipNavigator() {
		t.Error("Expected medium complexity task to NOT skip Navigator/self-review")
	}
}

// TestBuildPrompt_ConstantsSourced verifies pre-commit verification includes "Constants sourced" (GH-1321)
func TestBuildPrompt_ConstantsSourced(t *testing.T) {
	// Create a temp directory with .agent/ to simulate Navigator project
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	runner := NewRunner()

	task := &Task{
		ID:          "TEST-1321",
		Title:       "Add pricing constants",
		Description: "Add rate limits with proper validation",
		ProjectPath: tmpDir,
		Branch:      "pilot/TEST-1321",
	}

	prompt := runner.BuildPrompt(task, task.ProjectPath)

	if !strings.Contains(prompt, "Constants sourced") {
		t.Error("BuildPrompt should contain 'Constants sourced' verification item")
	}
	if !strings.Contains(prompt, "new code tested") {
		t.Error("BuildPrompt should contain 'new code tested' in tests verification item")
	}
}

// TestBuildSelfReviewPrompt_ConstantValueSanity verifies self-review includes check #6 (GH-1321)
func TestBuildSelfReviewPrompt_ConstantValueSanity(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TEST-1321",
		Title:       "Test task",
		Description: "Test description",
		ProjectPath: "/tmp/test",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	if !strings.Contains(prompt, "Constant Value Sanity Check") {
		t.Error("Self-review prompt should contain 'Constant Value Sanity Check'")
	}
	if !strings.Contains(prompt, "SUSPICIOUS_VALUE") {
		t.Error("Self-review prompt should contain 'SUSPICIOUS_VALUE' signal")
	}
}

// TestBuildSelfReviewPrompt_CrossFileParity verifies self-review includes check #7 (GH-1321)
func TestBuildSelfReviewPrompt_CrossFileParity(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TEST-1321",
		Title:       "Test task",
		Description: "Test description",
		ProjectPath: "/tmp/test",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	if !strings.Contains(prompt, "Cross-File Parity Check") {
		t.Error("Self-review prompt should contain 'Cross-File Parity Check'")
	}
	if !strings.Contains(prompt, "PARITY_GAP") {
		t.Error("Self-review prompt should contain 'PARITY_GAP' signal")
	}
}
