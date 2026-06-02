package executor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// --- Runner integration tests ---

func TestRecordGraphLearning_Success(t *testing.T) {
	runner := NewRunner()
	mock := &mockKnowledgeGraphRecorder{}
	runner.SetKnowledgeGraph(mock)

	task := &Task{
		ID:          "GH-100",
		Title:       "Add webhook auth",
		Description: "Add authentication to webhook endpoints",
	}

	result := &ExecutionResult{
		Success:  true,
		Duration: 3 * time.Second,
	}

	runner.recordGraphLearning(task, result)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.learningCalls) != 1 {
		t.Fatalf("expected 1 AddExecutionLearning call, got %d", len(mock.learningCalls))
	}

	call := mock.learningCalls[0]
	if call.title != "Add webhook auth" {
		t.Errorf("title = %q, want %q", call.title, "Add webhook auth")
	}
	if call.outcome != "success" {
		t.Errorf("outcome = %q, want %q", call.outcome, "success")
	}
	// Should extract "auth" and "webhook" patterns
	hasAuth := false
	hasWebhook := false
	for _, p := range call.patterns {
		if p == "auth" {
			hasAuth = true
		}
		if p == "webhook" {
			hasWebhook = true
		}
	}
	if !hasAuth {
		t.Error("expected 'auth' in patterns")
	}
	if !hasWebhook {
		t.Error("expected 'webhook' in patterns")
	}
}

func TestRecordGraphLearning_Failure(t *testing.T) {
	runner := NewRunner()
	mock := &mockKnowledgeGraphRecorder{}
	runner.SetKnowledgeGraph(mock)

	task := &Task{
		ID:          "GH-101",
		Title:       "Fix database migration",
		Description: "Fix broken migration script",
	}

	result := &ExecutionResult{
		Success: false,
		Error:   "compilation failed",
	}

	runner.recordGraphLearning(task, result)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.learningCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.learningCalls))
	}
	if mock.learningCalls[0].outcome != "failure" {
		t.Errorf("outcome = %q, want %q", mock.learningCalls[0].outcome, "failure")
	}
}

func TestRecordGraphLearning_NilGraph(t *testing.T) {
	runner := NewRunner()
	// knowledgeGraph is nil by default
	task := &Task{ID: "GH-102", Title: "Test nil"}
	result := &ExecutionResult{Success: true}

	// Should not panic
	runner.recordGraphLearning(task, result)
}

func TestRecordGraphLearning_ErrorDoesNotPanic(t *testing.T) {
	runner := NewRunner()
	mock := &mockKnowledgeGraphRecorder{returnErr: fmt.Errorf("disk full")}
	runner.SetKnowledgeGraph(mock)

	task := &Task{ID: "GH-103", Title: "Test error"}
	result := &ExecutionResult{Success: true}

	// Should not panic - error is logged
	runner.recordGraphLearning(task, result)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.learningCalls) != 1 {
		t.Fatalf("expected call even on error, got %d", len(mock.learningCalls))
	}
}

func TestRecordGraphLearning_TruncatesLongDescription(t *testing.T) {
	runner := NewRunner()
	mock := &mockKnowledgeGraphRecorder{}
	runner.SetKnowledgeGraph(mock)

	longDesc := strings.Repeat("x", 600)
	task := &Task{
		ID:          "GH-104",
		Title:       "Long task",
		Description: longDesc,
	}
	result := &ExecutionResult{Success: true}

	runner.recordGraphLearning(task, result)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.learningCalls[0].content) > 500 {
		t.Errorf("content length = %d, want <= 500", len(mock.learningCalls[0].content))
	}
}

// --- Setter/Has tests ---

func TestSetKnowledgeGraph(t *testing.T) {
	runner := NewRunner()
	if runner.HasKnowledgeGraph() {
		t.Error("expected HasKnowledgeGraph=false before set")
	}
	runner.SetKnowledgeGraph(&mockKnowledgeGraphRecorder{})
	if !runner.HasKnowledgeGraph() {
		t.Error("expected HasKnowledgeGraph=true after set")
	}
}

// --- extractLearningPatterns tests ---

func TestExtractLearningPatterns(t *testing.T) {
	tests := []struct {
		name     string
		task     *Task
		wantAny  []string
		wantNone []string
	}{
		{
			name:    "auth and api task",
			task:    &Task{Title: "Add OAuth to API", Description: "authentication endpoint"},
			wantAny: []string{"auth", "api"},
		},
		{
			name:     "no matching patterns",
			task:     &Task{Title: "Update readme", Description: "improve docs"},
			wantNone: []string{"auth", "api", "test"},
		},
		{
			name:    "test and fix",
			task:    &Task{Title: "Fix broken tests", Description: "unit test failing"},
			wantAny: []string{"fix", "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLearningPatterns(tt.task)
			gotSet := make(map[string]bool)
			for _, p := range got {
				gotSet[p] = true
			}
			for _, want := range tt.wantAny {
				if !gotSet[want] {
					t.Errorf("expected pattern %q in result %v", want, got)
				}
			}
			for _, notWant := range tt.wantNone {
				if gotSet[notWant] {
					t.Errorf("unexpected pattern %q in result %v", notWant, got)
				}
			}
		})
	}
}

// --- Compile-time interface check ---

func TestKnowledgeGraphRecorder_Interface(t *testing.T) {
	// Verify that *memory.KnowledgeGraph satisfies the KnowledgeGraphRecorder interface
	var _ KnowledgeGraphRecorder = (*memory.KnowledgeGraph)(nil)
}
