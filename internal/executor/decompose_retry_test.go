package executor

import (
	"context"
	"strings"
	"testing"
)

func TestDecomposeForRetry_BypassesAllGates(t *testing.T) {
	// DecomposeForRetry should bypass word count and complexity gates entirely.
	// A short task with numbered steps should decompose because execution failure
	// already proved the task is too large — gate bypass is the point of this path.
	config := &DecomposeConfig{
		Enabled:             false, // Disabled for normal path — bypass is the point
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 1000, // Very high threshold — would block normal decomposition
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "GH-1716",
		Title: "Implement auth system",
		Description: `Short task with numbered steps:
1. Add user model
2. Add login endpoint
3. Add session middleware`,
		ProjectPath: "/test/project",
	}

	result := decomposer.DecomposeForRetry(nil, task)

	if !result.Decomposed {
		t.Errorf("Expected DecomposeForRetry to decompose task with numbered steps, reason: %s", result.Reason)
	}
	if len(result.Subtasks) != 3 {
		t.Errorf("Expected 3 subtasks, got %d", len(result.Subtasks))
	}
	if result.Reason != "decomposed after execution failure (retry fallback)" {
		t.Errorf("Unexpected reason: %q", result.Reason)
	}
}

func TestDecomposeForRetry_RespectsNoDecomposeLabel(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:     true,
		MaxSubtasks: 5,
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:     "GH-1716",
		Title:  "Implement auth system",
		Labels: []string{NoDecomposeLabel},
		Description: `1. Add user model
2. Add login endpoint
3. Add session middleware`,
	}

	result := decomposer.DecomposeForRetry(nil, task)

	if result.Decomposed {
		t.Error("Expected DecomposeForRetry to respect no-decompose label")
	}
	if result.Reason != "skipped: no-decompose label (even on retry)" {
		t.Errorf("Unexpected reason: %q", result.Reason)
	}
}

func TestDecomposeForRetry_NoStructuralPoints(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:     true,
		MaxSubtasks: 5,
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "GH-1716",
		Title: "Refactor the system",
		Description: `This is a complex refactoring task that involves updating the system
architecture to support new requirements. The changes will touch multiple files
and modules across the codebase. We need to ensure backward compatibility while
improving the overall design.`,
	}

	result := decomposer.DecomposeForRetry(nil, task)

	if result.Decomposed {
		t.Error("Expected DecomposeForRetry to return false when no structural split points exist")
	}
	if result.Reason != "no decomposition points found (retry fallback)" {
		t.Errorf("Unexpected reason: %q", result.Reason)
	}
}

func TestHasNoDecomposePhrase(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		body     string
		expected bool
	}{
		// Each canonical phrase in body → true
		{name: "single AC list in body", body: "This is a single AC list for the task.", expected: true},
		{name: "do not decompose in body", body: "Please do not decompose this issue.", expected: true},
		{name: "do not split in body", body: "do not split this task.", expected: true},
		{name: "single Pilot issue in body", body: "This is a single Pilot issue.", expected: true},
		{name: "keep as ... single in body", body: "Keep as one single task.", expected: true},
		{name: "splitting this would in body", body: "Splitting this would fragment the changes.", expected: true},
		{name: "HTML marker in body", body: "<!-- pilot:no-decompose -->", expected: true},
		// Each canonical phrase in title → true
		{name: "do not decompose in title", title: "do not decompose this", expected: true},
		{name: "do not split in title", title: "do not split this", expected: true},
		{name: "single Pilot issue in title", title: "single Pilot issue with multiple steps", expected: true},
		{name: "splitting this would in title", title: "splitting this would break things", expected: true},
		// Mixed-case → true
		{name: "mixed case single AC list", body: "This has a Single AC List of criteria.", expected: true},
		{name: "mixed case do not decompose", body: "DO NOT DECOMPOSE this task.", expected: true},
		{name: "mixed case HTML marker", body: "<!-- Pilot:No-Decompose -->", expected: true},
		// HTML marker with extra whitespace → true
		{name: "HTML marker with spaces", body: "<!--  pilot:no-decompose  -->", expected: true},
		// Phrase as unrelated substring → false
		{name: "single word only", body: "This is a single change to a file.", expected: false},
		{name: "split without do not", body: "We should split this into modules.", expected: false},
		{name: "pilot issue without single", body: "This is a pilot issue for adding auth.", expected: false},
		{name: "keep without single", body: "Keep as is without further changes.", expected: false},
		// Empty → false
		{name: "empty body", title: "", body: "", expected: false},
		// Genuine multi-scope body without opt-out → false
		{
			name:     "genuine multi-scope body",
			title:    "Refactor auth and payments",
			body:     "Update internal/auth/handler.go and internal/payments/service.go to use the new middleware.",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				Title:       tt.title,
				Description: tt.body,
			}
			got := HasNoDecomposePhrase(task)
			if got != tt.expected {
				t.Errorf("HasNoDecomposePhrase() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestDecomposeWithContext_LLMClassifierSkipsWordCountGate verifies that when an LLM
// classifier returns COMPLEX, the word count gate is bypassed (GH-1728).
func TestDecomposeWithContext_LLMClassifierSkipsWordCountGate(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 300, // High threshold — task description is well under this
	}
	decomposer := NewTaskDecomposer(config)

	// Attach LLM classifier that always returns COMPLEX
	classifier := newComplexityClassifierWithRunner(mockClaudeRunner("COMPLEX", "multiple adapter changes required"))
	decomposer.SetClassifier(classifier)

	// Short description (~30 words) with numbered steps — well under MinDescriptionWords
	task := &Task{
		ID:    "GH-1716",
		Title: "Add webhook support to three adapters",
		Description: `Add outbound webhook support to three adapters:
1. Telegram adapter
2. Slack adapter
3. GitHub adapter`,
		ProjectPath: "/test/project",
	}

	result := decomposer.DecomposeWithContext(context.Background(), task)

	if !result.Decomposed {
		t.Errorf("LLM classifier COMPLEX + short description should decompose; reason: %s", result.Reason)
	}
	if len(result.Subtasks) != 3 {
		t.Errorf("Expected 3 subtasks (one per adapter), got %d", len(result.Subtasks))
	}
}

// TestDecomposeWithContext_HeuristicEnforcesWordCountGate verifies that without an LLM
// classifier the word count gate still blocks short descriptions (GH-1728).
func TestDecomposeWithContext_HeuristicEnforcesWordCountGate(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 300, // High threshold
	}
	decomposer := NewTaskDecomposer(config)
	// No classifier set — heuristic mode

	task := &Task{
		ID:    "GH-1716",
		Title: "Refactor all three adapters completely",
		Description: `Refactor all three adapters completely:
1. Telegram adapter
2. Slack adapter
3. GitHub adapter`,
		ProjectPath: "/test/project",
	}

	result := decomposer.DecomposeWithContext(context.Background(), task)

	if result.Decomposed {
		t.Error("Heuristic mode with short description should NOT decompose")
	}
	if !strings.Contains(result.Reason, "heuristic mode") {
		t.Errorf("Expected reason to mention heuristic mode, got %q", result.Reason)
	}
}
