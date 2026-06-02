package memory

import (
	"os"
	"testing"
)

func TestKnowledgeGraph_AddPattern(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	metadata := map[string]interface{}{"language": "go"}
	if err := kg.AddPattern("error_handling", "Always check errors", metadata); err != nil {
		t.Fatalf("AddPattern() error = %v", err)
	}

	patterns := kg.GetPatterns()
	if len(patterns) != 1 {
		t.Fatalf("GetPatterns() returned %d, want 1", len(patterns))
	}

	if patterns[0].Title != "error_handling" {
		t.Errorf("Pattern Title = %q, want 'error_handling'", patterns[0].Title)
	}

	if patterns[0].Content != "Always check errors" {
		t.Errorf("Pattern Content = %q, want 'Always check errors'", patterns[0].Content)
	}
}

func TestKnowledgeGraph_AddLearning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	metadata := map[string]interface{}{"project": "pilot"}
	if err := kg.AddLearning("Context cancellation", "Always propagate context", metadata); err != nil {
		t.Fatalf("AddLearning() error = %v", err)
	}

	learnings := kg.GetLearnings()
	if len(learnings) != 1 {
		t.Fatalf("GetLearnings() returned %d, want 1", len(learnings))
	}

	if learnings[0].Title != "Context cancellation" {
		t.Errorf("Learning Title = %q, want 'Context cancellation'", learnings[0].Title)
	}
}

func TestKnowledgeGraph_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create and populate graph
	kg1, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	node := &GraphNode{
		ID:      "persist-test",
		Type:    "learning",
		Title:   "Persistent Node",
		Content: "Should survive reload",
	}
	if err := kg1.Add(node); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Load new graph from same path
	kg2, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() reload error = %v", err)
	}

	got, ok := kg2.Get("persist-test")
	if !ok {
		t.Fatal("Get() after reload returned ok=false")
	}

	if got.Title != "Persistent Node" {
		t.Errorf("Title after reload = %q, want %q", got.Title, "Persistent Node")
	}

	if got.Content != "Should survive reload" {
		t.Errorf("Content after reload = %q, want %q", got.Content, "Should survive reload")
	}
}

func TestKnowledgeGraph_UpdateExistingNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	// Add initial node
	node := &GraphNode{
		ID:      "update-test",
		Type:    "pattern",
		Title:   "Original Title",
		Content: "Original Content",
	}
	if err := kg.Add(node); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got, _ := kg.Get("update-test")
	originalCreatedAt := got.CreatedAt

	// Update node
	updatedNode := &GraphNode{
		ID:      "update-test",
		Type:    "pattern",
		Title:   "Updated Title",
		Content: "Updated Content",
	}
	if err := kg.Add(updatedNode); err != nil {
		t.Fatalf("Add() update error = %v", err)
	}

	got, _ = kg.Get("update-test")

	if got.Title != "Updated Title" {
		t.Errorf("Title after update = %q, want %q", got.Title, "Updated Title")
	}

	if !got.CreatedAt.Equal(originalCreatedAt) {
		t.Error("CreatedAt should be preserved on update")
	}

	if !got.UpdatedAt.After(originalCreatedAt) {
		t.Error("UpdatedAt should be after CreatedAt after update")
	}
}
