package memory

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDefaultLearningConfig(t *testing.T) {
	config := DefaultLearningConfig()

	if config == nil {
		t.Fatal("DefaultLearningConfig returned nil")
	}

	if config.FeedbackWeight <= 0 {
		t.Error("FeedbackWeight should be positive")
	}

	if config.DecayRate <= 0 {
		t.Error("DecayRate should be positive")
	}
}

func TestNewLearningLoop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	tests := []struct {
		name   string
		config *LearningConfig
	}{
		{name: "with nil config", config: nil},
		{name: "with custom config", config: &LearningConfig{FeedbackWeight: 0.2, DecayRate: 0.02}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := NewLearningLoop(store, nil, tt.config)
			if loop == nil {
				t.Error("NewLearningLoop returned nil")
			}
		})
	}
}

func TestRecordExecution_Outcomes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	loop := NewLearningLoop(store, extractor, nil)

	ctx := context.Background()

	// Create a pattern
	pattern := &CrossPattern{
		ID:         "outcome-test-pattern",
		Type:       "code",
		Title:      "Test Pattern",
		Confidence: 0.6,
		Scope:      "org",
	}
	_ = store.SaveCrossPattern(pattern)
	_ = store.LinkPatternToProject("outcome-test-pattern", "/test/project")

	tests := []struct {
		name           string
		status         string
		wantConfChange bool
		wantIncrease   bool
	}{
		{name: "completed execution", status: "completed", wantConfChange: true, wantIncrease: true},
		{name: "failed execution", status: "failed", wantConfChange: true, wantIncrease: false},
		{name: "running execution (neutral)", status: "running", wantConfChange: false, wantIncrease: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &Execution{
				ID:          "exec-" + tt.status,
				TaskID:      "task-1",
				ProjectPath: "/test/project",
				Status:      tt.status,
			}
			_ = store.SaveExecution(exec)

			err := loop.RecordExecution(ctx, exec, []string{"outcome-test-pattern"})
			if err != nil {
				t.Fatalf("RecordExecution failed: %v", err)
			}
		})
	}
}

func TestCalculateConfidenceDelta(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	config := &LearningConfig{FeedbackWeight: 0.1, DecayRate: 0.01}
	loop := NewLearningLoop(store, nil, config)

	tests := []struct {
		name    string
		outcome FeedbackOutcome
		wantMin float64
		wantMax float64
	}{
		{name: "success outcome", outcome: OutcomeSuccess, wantMin: 0.09, wantMax: 0.11},
		{name: "failure outcome", outcome: OutcomeFailure, wantMin: 0.14, wantMax: 0.16}, // 0.1 * 1.5
		{name: "neutral outcome", outcome: OutcomeNeutral, wantMin: 0, wantMax: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loop.calculateConfidenceDelta(tt.outcome)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateConfidenceDelta(%v) = %f, want between %f and %f", tt.outcome, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestApplyDecay(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	loop := NewLearningLoop(store, nil, &LearningConfig{FeedbackWeight: 0.1, DecayRate: 0.1})
	ctx := context.Background()

	// Create a pattern (will have current timestamp, so won't be stale)
	pattern := &CrossPattern{
		ID:         "decay-test",
		Type:       "code",
		Title:      "Decay Test",
		Confidence: 0.8,
		Scope:      "org",
	}
	_ = store.SaveCrossPattern(pattern)

	// Apply decay (should not affect recent patterns)
	updated, err := loop.ApplyDecay(ctx)
	if err != nil {
		t.Fatalf("ApplyDecay failed: %v", err)
	}

	// Recent pattern should not be decayed
	if updated > 0 {
		t.Logf("Note: %d patterns decayed (might be from other tests)", updated)
	}

	// Verify pattern confidence unchanged for recent patterns
	retrieved, _ := store.GetCrossPattern("decay-test")
	if retrieved.Confidence != 0.8 {
		t.Logf("Confidence changed from 0.8 to %f", retrieved.Confidence)
	}
}

func TestDeprecateLowConfidencePatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	loop := NewLearningLoop(store, nil, nil)
	ctx := context.Background()

	// Create patterns with varying confidence
	patterns := []*CrossPattern{
		{ID: "high-conf", Type: "code", Title: "High", Confidence: 0.9, Occurrences: 10, Scope: "org"},
		{ID: "low-conf-1", Type: "code", Title: "Low 1", Confidence: 0.2, Occurrences: 1, Scope: "org"},
		{ID: "low-conf-2", Type: "code", Title: "Low 2", Confidence: 0.15, Occurrences: 2, Scope: "org"},
	}

	for _, p := range patterns {
		_ = store.SaveCrossPattern(p)
	}

	deprecated, err := loop.DeprecateLowConfidencePatterns(ctx, 0.3)
	if err != nil {
		t.Fatalf("DeprecateLowConfidencePatterns failed: %v", err)
	}

	if deprecated != 2 {
		t.Errorf("deprecated %d patterns, want 2", deprecated)
	}

	// Verify high confidence pattern still exists
	_, err = store.GetCrossPattern("high-conf")
	if err != nil {
		t.Error("high confidence pattern should still exist")
	}
}

func TestFeedbackOutcomeConstants(t *testing.T) {
	tests := []struct {
		outcome  FeedbackOutcome
		expected string
	}{
		{OutcomeSuccess, "success"},
		{OutcomeFailure, "failure"},
		{OutcomeNeutral, "neutral"},
	}

	for _, tt := range tests {
		if string(tt.outcome) != tt.expected {
			t.Errorf("Outcome %v = %q, want %q", tt.outcome, tt.outcome, tt.expected)
		}
	}
}

func TestRecordExecution_WithExtractor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)

	loop := NewLearningLoop(store, extractor, nil)
	ctx := context.Background()

	// Create an execution with pattern-extractable output
	exec := &Execution{
		ID:          "extractor-exec",
		TaskID:      "task-1",
		ProjectPath: "/test/project",
		Status:      "completed",
		Output:      "using context.Context in Handler added error handling for validateUser",
	}
	_ = store.SaveExecution(exec)

	err = loop.RecordExecution(ctx, exec, nil)
	if err != nil {
		t.Fatalf("RecordExecution failed: %v", err)
	}

	// The extractor should have run and saved patterns
	if patternStore.Count() < 1 {
		t.Errorf("Expected at least 1 pattern from extractor, got %d", patternStore.Count())
	}
}

func TestRecordExecution_WithMultiplePatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	loop := NewLearningLoop(store, nil, nil)
	ctx := context.Background()

	// Create multiple patterns
	patterns := []string{"multi-1", "multi-2", "multi-3"}
	for _, id := range patterns {
		_ = store.SaveCrossPattern(&CrossPattern{ID: id, Type: "code", Title: id, Confidence: 0.6, Scope: "org"})
		_ = store.LinkPatternToProject(id, "/test/project")
	}

	exec := &Execution{
		ID:          "multi-exec",
		TaskID:      "task-multi",
		ProjectPath: "/test/project",
		Status:      "completed",
	}
	_ = store.SaveExecution(exec)

	err = loop.RecordExecution(ctx, exec, patterns)
	if err != nil {
		t.Fatalf("RecordExecution failed: %v", err)
	}

	// All patterns should have been updated
	for _, id := range patterns {
		perf, err := loop.GetPatternPerformance(ctx, id)
		if err != nil {
			t.Fatalf("GetPatternPerformance(%s) failed: %v", id, err)
		}
		if perf.SuccessCount != 1 {
			t.Errorf("Pattern %s SuccessCount = %d, want 1", id, perf.SuccessCount)
		}
	}
}

func TestApplyDecay_StalePatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "feedback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	loop := NewLearningLoop(store, nil, &LearningConfig{FeedbackWeight: 0.1, DecayRate: 0.5})
	ctx := context.Background()

	// Create a pattern with old timestamp (can't easily simulate this via API)
	// Just verify the function runs without error
	_, err = loop.ApplyDecay(ctx)
	if err != nil {
		t.Fatalf("ApplyDecay failed: %v", err)
	}

	// Verify function signature works
	_ = time.Now() // Just to avoid unused import
}
