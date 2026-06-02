package executor

import (
	"context"
	"errors"
	"testing"
)

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name     string
		task     *Task
		label    string
		expected bool
	}{
		{
			name:     "nil task",
			task:     nil,
			label:    "no-decompose",
			expected: false,
		},
		{
			name:     "no labels",
			task:     &Task{Labels: nil},
			label:    "no-decompose",
			expected: false,
		},
		{
			name:     "label present",
			task:     &Task{Labels: []string{"pilot", "no-decompose", "priority:high"}},
			label:    "no-decompose",
			expected: true,
		},
		{
			name:     "case insensitive",
			task:     &Task{Labels: []string{"No-Decompose"}},
			label:    "no-decompose",
			expected: true,
		},
		{
			name:     "label absent",
			task:     &Task{Labels: []string{"pilot", "enhancement"}},
			label:    "no-decompose",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasLabel(tt.task, tt.label)
			if result != tt.expected {
				t.Errorf("HasLabel() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectComplexity_NoDecomposePreventsEpic(t *testing.T) {
	task := &Task{
		Title:       "[epic] Large refactor across multiple systems",
		Description: "Phase 1: update models. Phase 2: rewrite API. Phase 3: migrate data. Phase 4: update frontend. Phase 5: integration tests.",
		Labels:      []string{"pilot", "no-decompose"},
	}

	result := DetectComplexity(task)
	if result != ComplexityComplex {
		t.Errorf("expected ComplexityComplex when no-decompose label present, got %s", result)
	}

	// Verify same task WITHOUT no-decompose is detected as epic
	taskWithoutLabel := &Task{
		Title:       task.Title,
		Description: task.Description,
		Labels:      []string{"pilot"},
	}
	result2 := DetectComplexity(taskWithoutLabel)
	if result2 != ComplexityEpic {
		t.Errorf("expected ComplexityEpic without no-decompose label, got %s", result2)
	}
}

func TestDecomposer_NoDecomposeLabel(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)

	// Task that would normally decompose (complex + numbered steps)
	task := &Task{
		ID:    "GH-600",
		Title: "Refactor authentication system",
		Description: `Refactor the entire authentication system:
1. Update user model
2. Rewrite login endpoint
3. Add session middleware
4. Update frontend components`,
		Labels: []string{"pilot", "no-decompose"},
	}

	result := decomposer.Decompose(task)
	if result.Decomposed {
		t.Error("expected task with no-decompose label to NOT be decomposed")
	}
	if result.Reason != "skipped: no-decompose label" {
		t.Errorf("expected reason 'skipped: no-decompose label', got %q", result.Reason)
	}
}

func TestDecomposer_WithLLMClassifier(t *testing.T) {
	// LLM says MEDIUM → should NOT decompose (threshold is complex)
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)
	classifier := newComplexityClassifierWithRunner(mockClaudeRunner("MEDIUM", "well-scoped feature with clear instructions"))
	decomposer.SetClassifier(classifier)

	// This task has numbered steps and "refactor" keyword — heuristic would say COMPLEX
	// But LLM correctly identifies it as MEDIUM (well-scoped)
	task := &Task{
		ID:    "GH-700",
		Title: "Add retry logic to webhook handler",
		Description: `Add retry logic to the webhook handler with exponential backoff:
1. Add retry counter to webhook event struct
2. Implement exponential backoff calculation
3. Add retry queue worker
4. Update webhook handler to enqueue failed events
5. Add unit tests for retry logic

Follow existing patterns in internal/webhooks/handler.go.`,
	}

	result := decomposer.DecomposeWithContext(context.Background(), task)
	if result.Decomposed {
		t.Error("expected LLM MEDIUM classification to prevent decomposition")
	}
	if result.Reason != "complexity below threshold: medium" {
		t.Errorf("expected threshold reason, got %q", result.Reason)
	}
}

func TestDecomposer_LLMClassifierFallback(t *testing.T) {
	// LLM returns error → falls back to heuristic
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)
	classifier := newComplexityClassifierWithRunner(mockClaudeRunnerError(errors.New("subprocess failed")))
	decomposer.SetClassifier(classifier)

	// This task has "refactor" keyword → heuristic says COMPLEX → should decompose
	task := &Task{
		ID:    "GH-701",
		Title: "Refactor authentication system",
		Description: `Refactor the entire auth system:
1. Update user model with new fields
2. Rewrite login endpoint for MFA
3. Add session validation middleware
4. Update frontend auth components`,
	}

	result := decomposer.DecomposeWithContext(context.Background(), task)
	// Heuristic fallback: "refactor" keyword → COMPLEX → should decompose
	if !result.Decomposed {
		t.Errorf("expected heuristic fallback to decompose (refactor = complex), reason: %s", result.Reason)
	}
}
