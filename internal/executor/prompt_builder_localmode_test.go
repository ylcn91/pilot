package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestBuildPromptLocalMode(t *testing.T) {
	// GH-2103: LocalMode should use a standalone prompt even if .agent/ exists
	tempDir, err := os.MkdirTemp("", "pilot-test-local")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create .agent/ directory to simulate Navigator project
	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	runner := NewRunner()
	task := &Task{
		ID:          "LOCAL-123",
		Title:       "Fix bug locally",
		Description: "Fix the authentication bug in auth_test.go",
		ProjectPath: tempDir,
		LocalMode:   true,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Should contain task description
	if !strings.Contains(prompt, "## Task") {
		t.Error("LocalMode should contain task section")
	}
	if !strings.Contains(prompt, "Fix the authentication bug") {
		t.Error("LocalMode should contain task description")
	}

	// Should NOT contain Navigator/PR workflow elements
	if strings.Contains(prompt, "PILOT EXECUTION MODE") {
		t.Error("LocalMode should not contain PILOT EXECUTION MODE header")
	}
	if strings.Contains(prompt, "## Project Context") {
		t.Error("LocalMode should not inject project context")
	}
	if strings.Contains(prompt, "## Relevant SOPs") {
		t.Error("LocalMode should not inject SOPs")
	}
	if strings.Contains(prompt, "optionally CREATE PRs") {
		t.Error("LocalMode should not mention PR creation constraints")
	}

	// Should have phased execution structure
	if !strings.Contains(prompt, "## Phase 1: RECON") {
		t.Error("LocalMode should have mandatory recon phase")
	}
	if !strings.Contains(prompt, "## Phase 2: IMPLEMENT") {
		t.Error("LocalMode should have implementation phase")
	}
	if !strings.Contains(prompt, "## Phase 3: RECOVERY") {
		t.Error("LocalMode should have recovery phase")
	}
	if !strings.Contains(prompt, "## Environment") {
		t.Error("LocalMode should have environment section")
	}
}

func TestBuildPromptLocalModeNoOraclePaths(t *testing.T) {
	// GH-2393: prompt must not name oracle test paths; agent should discover
	// the spec from the workspace. Naming /tests/test_outputs.py overfits
	// local-mode prompts to a specific external harness.
	tempDir, err := os.MkdirTemp("", "pilot-test-local-oracle")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	runner := NewRunner()
	task := &Task{
		ID:          "LOCAL-2393",
		Title:       "Oracle compliance",
		Description: "Solve task",
		ProjectPath: tempDir,
		LocalMode:   true,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	forbidden := []string{"/tests", "test_outputs", "test.sh", "conftest"}
	for _, s := range forbidden {
		if strings.Contains(prompt, s) {
			t.Errorf("LocalMode prompt must not mention oracle path %q", s)
		}
	}

	if !strings.Contains(prompt, "Discover the spec") {
		t.Error("LocalMode prompt should instruct agent to discover the spec from the workspace")
	}
	if !strings.Contains(prompt, "Do NOT assume a specific test-file path") {
		t.Error("LocalMode prompt should warn against assuming a specific test-file path")
	}
}

func TestBuildPromptLocalModeWithoutTestFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pilot-test-local-notest")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	runner := NewRunner()
	task := &Task{
		ID:          "LOCAL-456",
		Title:       "Add feature",
		Description: "Add rate limiting to API endpoints",
		ProjectPath: tempDir,
		LocalMode:   true,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Should NOT include test-first instruction for non-test tasks
	if strings.Contains(prompt, "Write tests FIRST") {
		t.Error("LocalMode should not include test-first instruction when task doesn't mention test files")
	}
}

func TestBuildPromptLocalModeWithPatternContext(t *testing.T) {
	// LocalMode uses a standalone prompt — patterns are NOT injected
	tempDir, err := os.MkdirTemp("", "pilot-test-local-patterns")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create a memory store and save a pattern
	store, err := memory.NewStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	_ = store.SaveCrossPattern(&memory.CrossPattern{
		ID:          "test-pattern-1",
		Type:        "code",
		Title:       "Error Wrapping",
		Description: "Always wrap errors with context",
		Context:     "Go code",
		Confidence:  0.9,
		Occurrences: 10,
		Scope:       "org",
	})

	runner := NewRunner()
	runner.SetPatternContext(NewPatternContext(store))

	task := &Task{
		ID:          "LOCAL-789",
		Title:       "Fix auth bug",
		Description: "Fix authentication error handling",
		ProjectPath: tempDir,
		LocalMode:   true,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Should have standalone task section
	if !strings.Contains(prompt, "## Task") {
		t.Error("LocalMode with patterns should still have task section")
	}

	// LocalMode uses a standalone prompt — patterns are not injected
	// (local prompt is self-contained for sandbox execution)
	if !strings.Contains(prompt, "## Phase 1: RECON") {
		t.Error("LocalMode should have recon phase")
	}
}

func TestBuildPromptLocalModeWithKnowledgeGraph(t *testing.T) {
	// LocalMode uses a standalone prompt for task framing
	tempDir, err := os.MkdirTemp("", "pilot-test-local-kg")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	runner := NewRunner()
	mock := &mockKnowledgeGraphRecorder{
		keywordResults: []*memory.GraphNode{
			{Title: "Auth Pattern", Type: "pattern", Content: "Use JWT for stateless auth"},
			{Title: "API Design", Type: "pattern", Content: "Always validate input"},
		},
	}
	runner.SetKnowledgeGraph(mock)

	task := &Task{
		ID:          "LOCAL-KG-1",
		Title:       "Add API authentication endpoint",
		Description: "Implement OAuth authentication for the REST API",
		ProjectPath: tempDir,
		LocalMode:   true,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Should have standalone task section
	if !strings.Contains(prompt, "## Task") {
		t.Error("LocalMode with knowledge graph should have task section")
	}

	// GH-2147: KG learnings ARE injected into local mode (max 3)
	if !strings.Contains(prompt, "## Related Learnings") {
		t.Error("LocalMode should inject Related Learnings from knowledge graph")
	}
	if !strings.Contains(prompt, "Auth Pattern") {
		t.Error("Should include knowledge graph nodes")
	}
}

func TestBuildPromptLocalModeNilComponents(t *testing.T) {
	// Nil patternContext and knowledgeGraph should not panic
	runner := NewRunner()

	task := &Task{
		ID:          "LOCAL-NIL-1",
		Title:       "Simple task",
		Description: "A simple local task",
		LocalMode:   true,
	}

	// Should not panic with nil components
	prompt := runner.BuildPrompt(task, "")

	if !strings.Contains(prompt, "## Task") {
		t.Error("LocalMode with nil components should produce task section")
	}
	if !strings.Contains(prompt, "## Phase 1: RECON") {
		t.Error("LocalMode with nil components should have recon phase")
	}
}

func TestBuildPromptLocalModeSandbox(t *testing.T) {
	// Test local sandbox mode: problem-solving prompt without restrictive constraints
	tempDir, err := os.MkdirTemp("", "pilot-test-local")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	runner := NewRunner()
	task := &Task{
		ID:          "BENCH-1",
		Title:       "Extract text from gcode",
		Description: "Write the extracted text to /app/out.txt",
		ProjectPath: tempDir,
		LocalMode:   true,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Local mode should use phased execution
	if !strings.Contains(prompt, "## Task") {
		t.Error("Should contain task section")
	}
	if !strings.Contains(prompt, "## Phase 1: RECON") {
		t.Error("Should contain mandatory recon phase")
	}
	if !strings.Contains(prompt, "## Phase 2: IMPLEMENT") {
		t.Error("Should contain implementation phase")
	}
	if !strings.Contains(prompt, "## Phase 3: RECOVERY") {
		t.Error("Should contain recovery phase")
	}
	if !strings.Contains(prompt, "Discover the spec") {
		t.Error("Should instruct agent to discover the spec from the workspace (GH-2393)")
	}

	// Should have environment section with pre-installed deps
	if !strings.Contains(prompt, "Pre-installed") {
		t.Error("Should list pre-installed packages to avoid wasting time")
	}
	if !strings.Contains(prompt, "numpy") {
		t.Error("Should mention numpy as pre-installed")
	}
	// Should enforce checking before installing
	if !strings.Contains(prompt, "ALWAYS check first") {
		t.Error("Should tell agent to check before installing packages")
	}

	// Should have implementation guidance
	if !strings.Contains(prompt, "brute-force") {
		t.Error("Should prefer working brute-force over perfect theory")
	}
	if !strings.Contains(prompt, "STOP IMMEDIATELY") {
		t.Error("Should tell agent to stop after tests pass")
	}
	// Should enforce mandatory planning
	if !strings.Contains(prompt, "Write a plan") {
		t.Error("Should enforce mandatory planning before implementation")
	}
	// Should warn about memory
	if !strings.Contains(prompt, "2GB RAM") {
		t.Error("Should warn about container memory limit")
	}

	// Should NOT have restrictive PR constraints
	if strings.Contains(prompt, "ONLY create files explicitly mentioned") {
		t.Error("Local mode should not have restrictive file constraints")
	}
	if strings.Contains(prompt, "Do NOT create additional files") {
		t.Error("Local mode should not restrict file creation")
	}
	if strings.Contains(prompt, "Commit with format") {
		t.Error("Local mode should not have commit instructions")
	}
}
