package memory

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// insertCrossPatternWithUpdatedAt inserts a cross_patterns row with an explicit
// updated_at. SaveCrossPattern always stamps CURRENT_TIMESTAMP, so a direct
// insert is the only way to inject a stale timestamp without sleeping — the
// same technique TestGetContextualConfidence_DecayedPattern uses for last_used.
func insertCrossPatternWithUpdatedAt(t *testing.T, store *Store, id string, confidence float64, updatedAt time.Time) {
	t.Helper()
	_, err := store.db.Exec(`
		INSERT INTO cross_patterns (id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope, updated_at)
		VALUES (?, 'code', ?, '', '', '[]', ?, 1, 0, 'org', ?)
	`, id, "Decay "+id, confidence, updatedAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("insert cross pattern %q failed: %v", id, err)
	}
}

// TestApplyDecay_CrossesStaleThreshold drives ApplyDecay across the 3-month
// staleness boundary by injecting updated_at timestamps directly. Patterns
// updated before now-3mo decay by (1-decayRate); fresher patterns are untouched.
func TestApplyDecay_CrossesStaleThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "decay-gap-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Now()
	// Just on the fresh side of the 3-month threshold (not stale).
	freshTS := now.AddDate(0, -3, 0).Add(48 * time.Hour)
	// Comfortably past the 3-month threshold (stale).
	staleTS := now.AddDate(0, -4, 0)

	insertCrossPatternWithUpdatedAt(t, store, "fresh", 0.80, freshTS)
	insertCrossPatternWithUpdatedAt(t, store, "stale", 0.80, staleTS)

	loop := NewLearningLoop(store, nil, &LearningConfig{FeedbackWeight: 0.1, DecayRate: 0.1})

	updated, err := loop.ApplyDecay(context.Background())
	if err != nil {
		t.Fatalf("ApplyDecay failed: %v", err)
	}
	if updated != 1 {
		t.Fatalf("ApplyDecay updated %d patterns, want exactly 1 (only the stale one)", updated)
	}

	fresh, err := store.GetCrossPattern("fresh")
	if err != nil {
		t.Fatalf("GetCrossPattern(fresh) failed: %v", err)
	}
	if math.Abs(fresh.Confidence-0.80) > 1e-9 {
		t.Errorf("fresh pattern confidence = %.4f, want 0.80 (must not decay)", fresh.Confidence)
	}

	stale, err := store.GetCrossPattern("stale")
	if err != nil {
		t.Fatalf("GetCrossPattern(stale) failed: %v", err)
	}
	// 0.80 * (1 - 0.1) = 0.72
	wantStale := 0.80 * (1 - 0.1)
	if math.Abs(stale.Confidence-wantStale) > 1e-9 {
		t.Errorf("stale pattern confidence = %.6f, want %.6f", stale.Confidence, wantStale)
	}
}

// TestApplyDecay_FloorClamp verifies the 0.1 floor: a stale, already-low pattern
// cannot decay below 0.1 even when the multiplicative decay would push it lower.
func TestApplyDecay_FloorClamp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "decay-floor-gap-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	staleTS := time.Now().AddDate(0, -6, 0)
	// 0.105 * (1 - 0.5) = 0.0525 -> clamped to 0.1
	insertCrossPatternWithUpdatedAt(t, store, "lowstale", 0.105, staleTS)

	loop := NewLearningLoop(store, nil, &LearningConfig{FeedbackWeight: 0.1, DecayRate: 0.5})

	updated, err := loop.ApplyDecay(context.Background())
	if err != nil {
		t.Fatalf("ApplyDecay failed: %v", err)
	}
	if updated != 1 {
		t.Fatalf("ApplyDecay updated %d patterns, want 1", updated)
	}

	got, err := store.GetCrossPattern("lowstale")
	if err != nil {
		t.Fatalf("GetCrossPattern failed: %v", err)
	}
	if math.Abs(got.Confidence-0.1) > 1e-9 {
		t.Errorf("clamped confidence = %.6f, want 0.1 floor", got.Confidence)
	}
}
