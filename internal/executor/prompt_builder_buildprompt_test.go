package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptWithProjectContext(t *testing.T) {
	// Create temporary test environment
	tempDir, err := os.MkdirTemp("", "pilot-test-prompt")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	err = os.MkdirAll(agentDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	// Create mock DEVELOPMENT-README.md
	mockReadme := `# Test Project

### Key Components

| Component | Status |
|-----------|--------|
| Test Component | Done |

## Key Files

- test.go - Main test file

**Current Version:** v1.0.0
`
	readmePath := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	err = os.WriteFile(readmePath, []byte(mockReadme), 0644)
	if err != nil {
		t.Fatalf("Failed to write test README: %v", err)
	}

	// Create SOP files
	sopsDir := filepath.Join(agentDir, "sops")
	err = os.MkdirAll(sopsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create sops dir: %v", err)
	}

	testSOP := filepath.Join(sopsDir, "testing-guide.md")
	err = os.WriteFile(testSOP, []byte("Testing guide content"), 0644)
	if err != nil {
		t.Fatalf("Failed to write SOP file: %v", err)
	}

	// Test with Navigator project (has .agent/)
	runner := NewRunner()
	task := &Task{
		ID:          "TEST-123",
		Title:       "Add tests",
		Description: "Add unit testing for the module",
		ProjectPath: tempDir,
		Branch:      "pilot/TEST-123",
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Check that project context is included
	if !strings.Contains(prompt, "## Project Context") {
		t.Error("Prompt should contain project context section")
	}
	if !strings.Contains(prompt, "### Key Components") {
		t.Error("Prompt should contain key components from DEVELOPMENT-README.md")
	}
	if !strings.Contains(prompt, "Test Component") {
		t.Error("Prompt should contain specific content from README")
	}

	// Check that SOP hints are included
	if !strings.Contains(prompt, "## Relevant SOPs") {
		t.Error("Prompt should contain SOP hints section")
	}
	if !strings.Contains(prompt, "testing-guide.md") {
		t.Error("Prompt should contain matching SOP file")
	}

	// Check that it's in correct order (project context before task)
	contextPos := strings.Index(prompt, "## Project Context")
	taskPos := strings.Index(prompt, "## Task:")
	if contextPos == -1 || taskPos == -1 {
		t.Error("Both project context and task sections should be present")
	}
	if contextPos >= taskPos {
		t.Error("Project context should come before task description")
	}
}

func TestBuildPromptContainsErrcheckGuidance(t *testing.T) {
	// Create temporary test environment with .agent/
	tempDir, err := os.MkdirTemp("", "pilot-test-errcheck")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	runner := NewRunner()
	task := &Task{
		ID:          "GH-1797",
		Title:       "Add errcheck lint guidance",
		Description: "Add lint guidance to prompt builder",
		ProjectPath: tempDir,
		Branch:      "pilot/GH-1797",
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Verify pre-commit section contains errcheck guidance
	if !strings.Contains(prompt, "Lint compliance") {
		t.Error("BuildPrompt should contain lint compliance bullet in pre-commit verification")
	}
	if !strings.Contains(prompt, "errcheck") {
		t.Error("BuildPrompt should mention errcheck linter")
	}
	if !strings.Contains(prompt, "w.Write()") {
		t.Error("BuildPrompt should mention w.Write() as common unchecked return value")
	}
}

// GH-3224: every executor prompt must carry the evidence-backed-spec directive
// so the model does not silently no-op on explicit changes that "look correct."
func TestBuildPromptContainsEvidenceBackedSpecDirective(t *testing.T) {
	runner := NewRunner()

	cases := []struct {
		name string
		task *Task
	}{
		{
			name: "navigator path",
			task: &Task{ID: "GH-3224", Title: "fix no-op", Description: "Change line 22", Branch: "pilot/GH-3224"},
		},
		{
			name: "with acceptance criteria",
			task: &Task{ID: "GH-3224", Title: "fix no-op", Description: "Create new file", AcceptanceCriteria: []string{"file exists"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "pilot-test-noop")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tempDir) }()
			tc.task.ProjectPath = tempDir

			prompt := runner.BuildPrompt(tc.task, tempDir)

			for _, want := range []string{
				"NON-NEGOTIABLE",
				"evidence-backed",
				"NO-OP RATIONALE",
			} {
				if !strings.Contains(prompt, want) {
					t.Errorf("BuildPrompt missing %q from EvidenceBackedSpecDirective", want)
				}
			}
		})
	}
}

// TestBuildPromptExecutorHeader verifies the [PILOT-EXEC] executor-mode header
// is emitted for every non-image, non-local-mode task so the child Claude
// session (and project CLAUDE.md) can tell it was invoked by Pilot without
// sniffing CWD or prompt-prefix heuristics. GH-2328.
func TestBuildPromptExecutorHeader(t *testing.T) {
	runner := NewRunner()

	cases := []struct {
		name  string
		task  *Task
		setup func(t *testing.T) string
		want  bool
	}{
		{
			name: "navigator task has header",
			task: &Task{
				ID:          "GH-2328",
				Title:       "Signal executor mode",
				Description: "Signal executor mode to Claude",
				Branch:      "pilot/GH-2328",
			},
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.MkdirAll(filepath.Join(dir, ".agent"), 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return dir
			},
			want: true,
		},
		{
			name: "trivial navigator task has header",
			task: &Task{
				ID:          "TRIVIAL-1",
				Title:       "Fix typo",
				Description: "Fix typo in README.md",
			},
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.MkdirAll(filepath.Join(dir, ".agent"), 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return dir
			},
			want: true,
		},
		{
			name: "non-navigator project has header",
			task: &Task{
				ID:          "NONAV-1",
				Title:       "Add file",
				Description: "Create a config file",
			},
			setup: func(t *testing.T) string { return t.TempDir() },
			want:  true,
		},
		{
			name: "local mode does not include header",
			task: &Task{
				ID:          "LOCAL-1",
				Title:       "Sandbox run",
				Description: "Solve sandbox task",
				LocalMode:   true,
			},
			setup: func(t *testing.T) string { return t.TempDir() },
			want:  false,
		},
		{
			name: "image task does not include header",
			task: &Task{
				ID:          "IMG-1",
				Title:       "Describe image",
				Description: "Describe the image",
				ImagePath:   "/tmp/nonexistent.png",
			},
			setup: func(t *testing.T) string { return t.TempDir() },
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			tc.task.ProjectPath = dir

			prompt := runner.BuildPrompt(tc.task, dir)
			got := strings.HasPrefix(prompt, "[PILOT-EXEC]")
			if got != tc.want {
				t.Errorf("header present = %v, want %v\nprompt[:80]=%q",
					got, tc.want, prompt[:min(len(prompt), 80)])
			}

			if tc.want {
				// The body of the header must tell Claude not to defer. Without
				// this, a refusal-prone project CLAUDE.md could still veto the
				// task on mixed heuristics.
				if !strings.Contains(prompt, "do not refuse") {
					t.Error("executor header missing 'do not refuse' directive")
				}
				if !strings.Contains(prompt, "Navigator + Pilot pipeline") {
					t.Error("executor header should name the Navigator + Pilot pipeline")
				}
			}
		})
	}
}

func TestBuildPromptSkipsNavigatorForTrivialTask(t *testing.T) {
	// Create temporary test environment with .agent/
	tempDir, err := os.MkdirTemp("", "pilot-test-trivial")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	err = os.MkdirAll(agentDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	// Create trivial task (should skip Navigator overhead)
	runner := NewRunner()
	task := &Task{
		ID:          "TRIVIAL-123",
		Title:       "Fix typo",
		Description: "Fix typo in README.md",
		ProjectPath: tempDir,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Trivial tasks should skip project context even when .agent/ exists
	if strings.Contains(prompt, "## Project Context") {
		t.Error("Trivial task should not include project context to reduce overhead")
	}
	if strings.Contains(prompt, "## Relevant SOPs") {
		t.Error("Trivial task should not include SOP hints to reduce overhead")
	}

	// But should still have trivial task header
	if !strings.Contains(prompt, "PILOT EXECUTION MODE (Trivial Task)") {
		t.Error("Trivial task should have appropriate header")
	}
}

func TestBuildPromptNoNavigator(t *testing.T) {
	// Test with non-Navigator project (no .agent/ directory)
	tempDir, err := os.MkdirTemp("", "pilot-test-no-nav")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	// Don't create .agent/ directory

	runner := NewRunner()
	task := &Task{
		ID:          "NO-NAV-123",
		Title:       "Regular task",
		Description: "Regular development task",
		ProjectPath: tempDir,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Non-Navigator projects should not have project context
	if strings.Contains(prompt, "## Project Context") {
		t.Error("Non-Navigator project should not include project context")
	}
	if strings.Contains(prompt, "## Relevant SOPs") {
		t.Error("Non-Navigator project should not include SOP hints")
	}

	// Should have regular task structure
	if !strings.Contains(prompt, "## Task: NO-NAV-123") {
		t.Error("Should contain task ID")
	}
	if !strings.Contains(prompt, "Regular development task") {
		t.Error("Should contain task description")
	}
}

// A1: BuildPrompt inlines an excerpt for the top SOP match under its pointer.
func TestBuildPromptInlinesSOPExcerpt(t *testing.T) {
	tempDir := t.TempDir()
	agentDir := filepath.Join(tempDir, ".agent")
	sopsDir := filepath.Join(agentDir, "sops")
	if err := os.MkdirAll(sopsDir, 0755); err != nil {
		t.Fatalf("mkdir sops: %v", err)
	}
	sopBody := "SOP: run all tests before committing. Use table-driven tests."
	if err := os.WriteFile(filepath.Join(sopsDir, "testing-guide.md"), []byte(sopBody), 0644); err != nil {
		t.Fatalf("write sop: %v", err)
	}

	runner := NewRunner()
	task := &Task{
		ID:          "TEST-A1",
		Title:       "Add tests",
		Description: "Add unit testing for the module",
		ProjectPath: tempDir,
	}
	prompt := runner.BuildPrompt(task, tempDir)

	if !strings.Contains(prompt, "`.agent/sops/testing-guide.md`") {
		t.Error("expected SOP pointer line")
	}
	if !strings.Contains(prompt, "run all tests before committing") {
		t.Error("expected inlined SOP excerpt under the pointer")
	}
}
