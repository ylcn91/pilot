package executor

import (
	"context"
	"errors"
	"testing"
)

func TestComplexityClassifier_SimpleTask(t *testing.T) {
	classifier := newComplexityClassifierWithRunner(mockClaudeRunner("SIMPLE", "Single field addition"))
	task := &Task{
		ID:          "GH-100",
		Title:       "Add email field to user struct",
		Description: "Add an email field to the user struct in models.go",
	}

	result := classifier.Classify(context.Background(), task)
	if result != ComplexitySimple {
		t.Errorf("expected SIMPLE, got %s", result)
	}
}

func TestComplexityClassifier_MediumDetailedTask(t *testing.T) {
	// This is the key test: a detailed but well-scoped issue should be MEDIUM, not COMPLEX
	classifier := newComplexityClassifierWithRunner(mockClaudeRunner("MEDIUM", "Detailed instructions but single-scope feature"))
	task := &Task{
		ID:    "GH-200",
		Title: "Add webhook endpoint with retry logic",
		Description: `Implement a webhook endpoint that:
1. Accepts POST requests at /api/webhooks
2. Validates the payload signature using HMAC-SHA256
3. Stores the event in the database
4. Implements retry logic with exponential backoff (max 3 retries)
5. Returns 200 on success, 400 on invalid signature
6. Add unit tests for all paths

Use the existing http router and database connection.
Follow the patterns in internal/api/handlers.go.`,
	}

	result := classifier.Classify(context.Background(), task)
	if result != ComplexityMedium {
		t.Errorf("expected MEDIUM (detailed but well-scoped), got %s", result)
	}
}

func TestComplexityClassifier_ComplexTask(t *testing.T) {
	classifier := newComplexityClassifierWithRunner(mockClaudeRunner("COMPLEX", "Requires architectural changes across multiple systems"))
	task := &Task{
		ID:          "GH-300",
		Title:       "Migrate authentication from sessions to JWT",
		Description: "Rewrite the entire auth system from session-based to JWT. Requires database schema changes, new middleware, updated frontend token handling, and migration of existing sessions.",
	}

	result := classifier.Classify(context.Background(), task)
	if result != ComplexityComplex {
		t.Errorf("expected COMPLEX, got %s", result)
	}
}

func TestComplexityClassifier_CachesResult(t *testing.T) {
	callCount := 0
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		callCount++
		return []byte(`{"complexity":"MEDIUM","reason":"standard work"}`), nil
	}

	classifier := newComplexityClassifierWithRunner(runner)
	task := &Task{
		ID:          "GH-400",
		Title:       "Add logging",
		Description: "Add structured logging to the API layer",
	}

	// First call hits subprocess
	result1 := classifier.Classify(context.Background(), task)
	// Second call should use cache
	result2 := classifier.Classify(context.Background(), task)

	if result1 != result2 {
		t.Errorf("cached result differs: %s vs %s", result1, result2)
	}
	if callCount != 1 {
		t.Errorf("expected 1 subprocess call (cached), got %d", callCount)
	}
}

func TestComplexityClassifier_FallsBackOnError(t *testing.T) {
	classifier := newComplexityClassifierWithRunner(mockClaudeRunnerError(errors.New("subprocess failed")))
	task := &Task{
		ID:          "GH-500",
		Title:       "Fix typo in README",
		Description: "Fix typo in README.md",
	}

	// Should fall back to heuristic (word count < 10 → Simple; "typo" pattern → Trivial)
	result := classifier.Classify(context.Background(), task)
	if result != ComplexityTrivial {
		t.Errorf("expected fallback to heuristic (TRIVIAL), got %s", result)
	}
}

func TestComplexityClassifier_NilTask(t *testing.T) {
	classifier := newComplexityClassifierWithRunner(mockClaudeRunner("MEDIUM", "n/a"))
	result := classifier.Classify(context.Background(), nil)
	if result != ComplexityMedium {
		t.Errorf("expected MEDIUM for nil task, got %s", result)
	}
}
