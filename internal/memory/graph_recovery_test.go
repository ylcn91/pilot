package memory

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestKnowledgeGraph_AtomicWriteCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	before := atomic.LoadInt64(&kg.saveCount)
	if err := kg.AddExecutionLearning(
		"test learning", "content",
		[]string{"a.go", "b.go"},
		[]string{"p1", "p2"},
		"success",
	); err != nil {
		t.Fatalf("AddExecutionLearning() error = %v", err)
	}
	after := atomic.LoadInt64(&kg.saveCount)

	if got := after - before; got != 1 {
		t.Errorf("AddExecutionLearning triggered %d disk writes, want exactly 1", got)
	}
}

func TestKnowledgeGraph_BakRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	// First write: knowledge.json=[node-a], no .bak yet.
	if err := kg.Add(&GraphNode{ID: "node-a", Type: "learning", Title: "Node A"}); err != nil {
		t.Fatalf("Add node-a: %v", err)
	}
	// Second write: knowledge.json.bak=[node-a], knowledge.json=[node-a,node-b].
	if err := kg.Add(&GraphNode{ID: "node-b", Type: "learning", Title: "Node B"}); err != nil {
		t.Fatalf("Add node-b: %v", err)
	}

	primaryPath := filepath.Join(tmpDir, "knowledge.json")
	if err := os.WriteFile(primaryPath, []byte("not valid json {{"), 0644); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}

	kg2, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() after corruption error = %v", err)
	}

	if kg2.Count() == 0 {
		t.Fatal("expected recovery from .bak; got 0 nodes")
	}
	if _, ok := kg2.Get("node-a"); !ok {
		t.Error("expected node-a to be recovered from .bak")
	}
	// .bak should have been promoted back to primary.
	if _, statErr := os.Stat(primaryPath); statErr != nil {
		t.Errorf("primary file should exist after .bak promotion: %v", statErr)
	}
}

func TestKnowledgeGraph_CrashTruncation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	if err := kg.Add(&GraphNode{ID: "node-a", Type: "learning", Title: "Node A"}); err != nil {
		t.Fatalf("Add node-a: %v", err)
	}
	// Second write creates knowledge.json.bak=[node-a].
	if err := kg.Add(&GraphNode{ID: "node-b", Type: "learning", Title: "Node B"}); err != nil {
		t.Fatalf("Add node-b: %v", err)
	}

	// Simulate incomplete write: truncate primary to 0 bytes.
	primaryPath := filepath.Join(tmpDir, "knowledge.json")
	f, openErr := os.OpenFile(primaryPath, os.O_WRONLY|os.O_TRUNC, 0644)
	if openErr != nil {
		t.Fatalf("truncate primary: %v", openErr)
	}
	_ = f.Close()

	kg2, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() after truncation error = %v", err)
	}

	if kg2.Count() == 0 {
		t.Fatal("expected recovery from .bak after truncation; got 0 nodes")
	}
	if _, ok := kg2.Get("node-a"); !ok {
		t.Error("expected node-a to be recovered from .bak after truncation")
	}
}
