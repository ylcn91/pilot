package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// --- Prompt builder integration tests ---

func TestBuildPrompt_InjectsRelatedLearnings(t *testing.T) {
	// Create temp dir with .agent/ so Navigator path is triggered
	tempDir, err := os.MkdirTemp("", "pilot-test-graph")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create .agent dir: %v", err)
	}

	now := time.Now()
	mock := &mockKnowledgeGraphRecorder{
		keywordResults: []*memory.GraphNode{
			{ID: "1", Type: "execution_learning", Title: "Add auth middleware", Content: "Added JWT validation", UpdatedAt: now},
			{ID: "2", Type: "pattern", Title: "Error handling", Content: "Always wrap errors", UpdatedAt: now},
		},
	}

	runner := NewRunner()
	runner.SetKnowledgeGraph(mock)

	task := &Task{
		ID:          "GH-200",
		Title:       "Add auth to webhook",
		Description: "Implement authentication for webhook endpoints",
		ProjectPath: tempDir,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	if !strings.Contains(prompt, "## Related Learnings") {
		t.Error("expected '## Related Learnings' section in prompt")
	}
	if !strings.Contains(prompt, "Add auth middleware") {
		t.Error("expected learning title in prompt")
	}
	if !strings.Contains(prompt, "Added JWT validation") {
		t.Error("expected learning content in prompt")
	}
	if !strings.Contains(prompt, "Always wrap errors") {
		t.Error("expected second learning content in prompt")
	}
}

func TestBuildPrompt_NoLearningsWhenGraphNil(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pilot-test-graph-nil")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create .agent dir: %v", err)
	}

	runner := NewRunner()
	// knowledgeGraph is nil

	task := &Task{
		ID:          "GH-201",
		Title:       "Simple fix",
		Description: "Fix a typo in config",
		ProjectPath: tempDir,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	if strings.Contains(prompt, "## Related Learnings") {
		t.Error("should not inject learnings when graph is nil")
	}
}

func TestBuildPrompt_NoLearningsWhenEmptyResults(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pilot-test-graph-empty")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create .agent dir: %v", err)
	}

	mock := &mockKnowledgeGraphRecorder{
		keywordResults: nil, // no results
	}

	runner := NewRunner()
	runner.SetKnowledgeGraph(mock)

	task := &Task{
		ID:          "GH-202",
		Title:       "Add webhook handler",
		Description: "Create a new webhook handler",
		ProjectPath: tempDir,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	if strings.Contains(prompt, "## Related Learnings") {
		t.Error("should not inject learnings section when no results")
	}
}

func TestBuildPrompt_CapsLearningsAtFive(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pilot-test-graph-cap")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	agentDir := filepath.Join(tempDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create .agent dir: %v", err)
	}

	now := time.Now()
	nodes := make([]*memory.GraphNode, 8)
	for i := range nodes {
		nodes[i] = &memory.GraphNode{
			ID:        fmt.Sprintf("node-%d", i),
			Type:      "execution_learning",
			Title:     fmt.Sprintf("Learning %d", i),
			Content:   fmt.Sprintf("Content for learning %d", i),
			UpdatedAt: now,
		}
	}

	mock := &mockKnowledgeGraphRecorder{keywordResults: nodes}

	runner := NewRunner()
	runner.SetKnowledgeGraph(mock)

	task := &Task{
		ID:          "GH-203",
		Title:       "Add API auth",
		Description: "Implement api authentication",
		ProjectPath: tempDir,
	}

	prompt := runner.BuildPrompt(task, tempDir)

	// Should contain learnings 0-4 but NOT 5-7
	if !strings.Contains(prompt, "Learning 4") {
		t.Error("expected Learning 4 in prompt (5th item)")
	}
	if strings.Contains(prompt, "Learning 5") {
		t.Error("Learning 5 should be excluded (cap at 5)")
	}
}
