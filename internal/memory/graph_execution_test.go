package memory

import (
	"os"
	"testing"
	"time"
)

func TestKnowledgeGraph_AddExecutionLearning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	files := []string{"internal/executor/runner.go", "internal/executor/monitor.go"}
	patterns := []string{"error-handling", "retry-logic"}
	outcome := "success"

	if err := kg.AddExecutionLearning("Fix timeout bug", "Added retry with backoff", files, patterns, outcome); err != nil {
		t.Fatalf("AddExecutionLearning() error = %v", err)
	}

	// Should have: 1 learning + 2 file nodes + 2 pattern nodes + 1 outcome = 6
	if count := kg.Count(); count != 6 {
		t.Errorf("Count() = %d, want 6", count)
	}

	// Verify the learning node has relations
	learnings := kg.GetByType("execution_learning")
	if len(learnings) != 1 {
		t.Fatalf("GetByType('execution_learning') returned %d, want 1", len(learnings))
	}

	learning := learnings[0]
	if learning.Title != "Fix timeout bug" {
		t.Errorf("Title = %q, want %q", learning.Title, "Fix timeout bug")
	}
	if learning.Content != "Added retry with backoff" {
		t.Errorf("Content = %q, want %q", learning.Content, "Added retry with backoff")
	}
	// 2 files + 2 patterns + 1 outcome = 5 relations
	if len(learning.Relations) != 5 {
		t.Errorf("Relations count = %d, want 5", len(learning.Relations))
	}

	// Verify related nodes are retrievable
	related := kg.GetRelated(learning.ID)
	if len(related) != 5 {
		t.Errorf("GetRelated() returned %d, want 5", len(related))
	}

	// Verify metadata
	if learning.Metadata["outcome"] != "success" {
		t.Errorf("Metadata[outcome] = %v, want %q", learning.Metadata["outcome"], "success")
	}

	// Verify file nodes exist
	fileNodes := kg.GetByType("file")
	if len(fileNodes) != 2 {
		t.Errorf("GetByType('file') returned %d, want 2", len(fileNodes))
	}

	// Verify outcome node exists
	outcomeNodes := kg.GetByType("outcome")
	if len(outcomeNodes) != 1 {
		t.Errorf("GetByType('outcome') returned %d, want 1", len(outcomeNodes))
	}
	if outcomeNodes[0].Title != "success" {
		t.Errorf("Outcome Title = %q, want %q", outcomeNodes[0].Title, "success")
	}
}

func TestKnowledgeGraph_AddExecutionLearning_EmptyInputs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	// Empty files and patterns — should still create learning + outcome
	if err := kg.AddExecutionLearning("Minimal learning", "No files changed", nil, nil, "success"); err != nil {
		t.Fatalf("AddExecutionLearning() error = %v", err)
	}

	// 1 learning + 1 outcome = 2
	if count := kg.Count(); count != 2 {
		t.Errorf("Count() = %d, want 2", count)
	}

	learnings := kg.GetByType("execution_learning")
	if len(learnings) != 1 {
		t.Fatalf("GetByType('execution_learning') returned %d, want 1", len(learnings))
	}
	// Only the outcome relation
	if len(learnings[0].Relations) != 1 {
		t.Errorf("Relations count = %d, want 1", len(learnings[0].Relations))
	}
}

func TestKnowledgeGraph_GetRelatedByKeywords(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	// Add execution learnings with a small time gap to ensure ordering
	files1 := []string{"internal/executor/runner.go"}
	if err := kg.AddExecutionLearning("Fix timeout bug", "Added retry with exponential backoff", files1, []string{"retry"}, "success"); err != nil {
		t.Fatalf("AddExecutionLearning() error = %v", err)
	}

	// Small delay to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	files2 := []string{"internal/gateway/server.go"}
	if err := kg.AddExecutionLearning("Add WebSocket auth", "Token validation for WebSocket connections", files2, []string{"auth"}, "success"); err != nil {
		t.Fatalf("AddExecutionLearning() error = %v", err)
	}

	t.Run("search by title keyword", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"timeout"})
		if len(results) == 0 {
			t.Fatal("expected at least 1 result for keyword 'timeout'")
		}
		found := false
		for _, r := range results {
			if r.Title == "Fix timeout bug" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find 'Fix timeout bug' in results")
		}
	})

	t.Run("search by content keyword", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"backoff"})
		if len(results) == 0 {
			t.Fatal("expected at least 1 result for keyword 'backoff'")
		}
	})

	t.Run("search by metadata keyword", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"runner.go"})
		if len(results) == 0 {
			t.Fatal("expected at least 1 result for keyword 'runner.go'")
		}
	})

	t.Run("case insensitive search", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"WEBSOCKET"})
		if len(results) == 0 {
			t.Fatal("expected at least 1 result for case-insensitive 'WEBSOCKET'")
		}
	})

	t.Run("multiple keywords match union", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"timeout", "WebSocket"})
		// Should find nodes from both learnings
		if len(results) < 2 {
			t.Errorf("expected at least 2 results for multiple keywords, got %d", len(results))
		}
	})

	t.Run("sorted by recency", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"success"})
		if len(results) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(results))
		}
		// First result should be more recent than second
		if results[0].UpdatedAt.Before(results[1].UpdatedAt) {
			t.Error("results not sorted by recency (newest first)")
		}
	})

	t.Run("empty keywords returns nil", func(t *testing.T) {
		results := kg.GetRelatedByKeywords(nil)
		if results != nil {
			t.Errorf("expected nil for empty keywords, got %d results", len(results))
		}

		results = kg.GetRelatedByKeywords([]string{})
		if results != nil {
			t.Errorf("expected nil for empty slice, got %d results", len(results))
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		results := kg.GetRelatedByKeywords([]string{"nonexistent-keyword-xyz"})
		if len(results) != 0 {
			t.Errorf("expected 0 results for non-matching keyword, got %d", len(results))
		}
	})
}

func TestKnowledgeGraph_AddExecutionLearning_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create graph and add execution learning
	kg1, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	if err := kg1.AddExecutionLearning("Persistent learning", "Should survive reload", []string{"file.go"}, []string{"pattern-a"}, "success"); err != nil {
		t.Fatalf("AddExecutionLearning() error = %v", err)
	}

	// Reload from disk
	kg2, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() reload error = %v", err)
	}

	learnings := kg2.GetByType("execution_learning")
	if len(learnings) != 1 {
		t.Fatalf("after reload: GetByType('execution_learning') returned %d, want 1", len(learnings))
	}

	if learnings[0].Title != "Persistent learning" {
		t.Errorf("after reload: Title = %q, want %q", learnings[0].Title, "Persistent learning")
	}

	// Relations should persist
	if len(learnings[0].Relations) != 3 {
		t.Errorf("after reload: Relations count = %d, want 3", len(learnings[0].Relations))
	}

	// Related nodes should be retrievable after reload
	related := kg2.GetRelated(learnings[0].ID)
	if len(related) != 3 {
		t.Errorf("after reload: GetRelated() returned %d, want 3", len(related))
	}

	// Keywords search should work after reload
	results := kg2.GetRelatedByKeywords([]string{"Persistent"})
	if len(results) == 0 {
		t.Error("after reload: GetRelatedByKeywords found no results")
	}
}
