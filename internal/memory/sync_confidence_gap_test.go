package memory

import (
	"context"
	"math"
	"os"
	"testing"
)

// seedProjectLink inserts a pattern_projects row with explicit success/failure
// counts. aggregatePattern reads success_count/failure_count straight from this
// table to compute per-project success rate, so we set them directly rather than
// routing through RecordPatternFeedback (which has a FK to executions and would
// require seeding execution rows just to bump a counter).
func seedProjectLink(t *testing.T, store *Store, patternID, project string, successes, failures int) {
	t.Helper()
	_, err := store.db.Exec(`
		INSERT INTO pattern_projects (pattern_id, project_path, uses, success_count, failure_count, last_used)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, patternID, project, successes+failures, successes, failures)
	if err != nil {
		t.Fatalf("insert pattern_projects link (%s, %s) failed: %v", patternID, project, err)
	}
}

// TestSyncFromProject_AggregateConfidenceFormula pins the confidence formula in
// aggregatePattern: baseConfidence = 0.5 + projectCount*0.05, plus
// avgSuccessRate*0.3, capped at 0.95. SyncFromProject drives the aggregation;
// we assert the resulting AggregatedPattern.Confidence matches the closed form.
func TestSyncFromProject_AggregateConfidenceFormula(t *testing.T) {
	tests := []struct {
		name string
		// per-project (successCount, failureCount) for the single pattern
		projects map[string][2]int
		// expected aggregate occurrences (sum of uses, which == success+failure
		// recorded here because each feedback also bumps the link counters but
		// not uses; uses == 1 per LinkPatternToProject call)
		wantProjectCount int
		wantConfidence   float64
	}{
		{
			name: "single project full success",
			projects: map[string][2]int{
				"/proj/a": {4, 0},
			},
			wantProjectCount: 1,
			// base = 0.5 + 1*0.05 = 0.55; avgSuccess = 1.0; +0.3 => 0.85
			wantConfidence: 0.85,
		},
		{
			name: "two projects mixed success",
			projects: map[string][2]int{
				"/proj/a": {3, 1}, // 0.75
				"/proj/b": {1, 3}, // 0.25
			},
			wantProjectCount: 2,
			// base = 0.5 + 2*0.05 = 0.60; avgSuccess = (0.75+0.25)/2 = 0.5; +0.15 => 0.75
			wantConfidence: 0.75,
		},
		{
			name: "many projects high success hits 0.95 cap",
			projects: map[string][2]int{
				"/proj/a": {5, 0},
				"/proj/b": {5, 0},
				"/proj/c": {5, 0},
				"/proj/d": {5, 0},
				"/proj/e": {5, 0},
				"/proj/f": {5, 0},
			},
			wantProjectCount: 6,
			// base = 0.5 + 6*0.05 = 0.80; avgSuccess = 1.0; +0.3 => 1.10 capped at 0.95
			wantConfidence: 0.95,
		},
		{
			name: "all failures yields base only",
			projects: map[string][2]int{
				"/proj/a": {0, 4},
				"/proj/b": {0, 4},
			},
			wantProjectCount: 2,
			// base = 0.5 + 2*0.05 = 0.60; avgSuccess = 0.0; +0.0 => 0.60
			wantConfidence: 0.60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "sync-conf-gap-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			store, err := NewStore(tmpDir)
			if err != nil {
				t.Fatalf("NewStore failed: %v", err)
			}
			defer func() { _ = store.Close() }()

			ps, err := NewPatternSync(store, tmpDir)
			if err != nil {
				t.Fatalf("NewPatternSync failed: %v", err)
			}

			const patternID = "conf-pat"
			if err := store.SaveCrossPattern(&CrossPattern{
				ID:         patternID,
				Type:       "code",
				Title:      "Confidence formula pattern",
				Confidence: 0.7,
				Scope:      "org",
			}); err != nil {
				t.Fatalf("SaveCrossPattern failed: %v", err)
			}

			// Pick a deterministic project to sync from.
			var syncProject string
			for project, sf := range tt.projects {
				seedProjectLink(t, store, patternID, project, sf[0], sf[1])
				syncProject = project
			}

			if err := ps.SyncFromProject(context.Background(), syncProject); err != nil {
				t.Fatalf("SyncFromProject failed: %v", err)
			}

			agg, ok := ps.orgPatterns.Get(patternID)
			if !ok {
				t.Fatalf("aggregated pattern %q not found after sync", patternID)
			}

			if agg.ProjectCount != tt.wantProjectCount {
				t.Errorf("ProjectCount = %d, want %d", agg.ProjectCount, tt.wantProjectCount)
			}

			if math.Abs(agg.Confidence-tt.wantConfidence) > 1e-9 {
				t.Errorf("Confidence = %.10f, want %.10f", agg.Confidence, tt.wantConfidence)
			}

			// Confidence must never exceed the documented cap.
			if agg.Confidence > 0.95 {
				t.Errorf("Confidence %.4f exceeds 0.95 cap", agg.Confidence)
			}
		})
	}
}

// TestAggregatePattern_NoLinksLeavesDefaultConfidence verifies the guarded
// branch: when ProjectCount == 0 the formula is skipped and Confidence stays at
// its zero value (the AggregatedPattern is created without a confidence write).
func TestAggregatePattern_NoLinksLeavesDefaultConfidence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sync-conf-nolinks-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ps, err := NewPatternSync(store, tmpDir)
	if err != nil {
		t.Fatalf("NewPatternSync failed: %v", err)
	}

	p := &CrossPattern{ID: "no-links", Type: "code", Title: "No links", Confidence: 0.7, Scope: "org"}

	// Call aggregatePattern directly with an empty link slice to exercise the
	// ProjectCount == 0 guard.
	if err := ps.aggregatePattern(p, nil); err != nil {
		t.Fatalf("aggregatePattern failed: %v", err)
	}

	agg, ok := ps.orgPatterns.Get("no-links")
	if !ok {
		t.Fatal("aggregated pattern not found")
	}
	if agg.ProjectCount != 0 {
		t.Errorf("ProjectCount = %d, want 0", agg.ProjectCount)
	}
	if agg.Confidence != 0 {
		t.Errorf("Confidence = %.4f, want 0 (formula skipped when no links)", agg.Confidence)
	}
}
