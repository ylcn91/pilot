package memory

import (
	"os"
	"testing"
	"time"
)

func TestRecordPatternOutcomeAndContextualConfidence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pattern-perf-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	tests := []struct {
		name      string
		setup     func(t *testing.T)
		patternID string
		projectID string
		taskType  string
		wantMin   float64
		wantMax   float64
	}{
		{
			name:      "fresh pattern returns default 0.5",
			setup:     func(t *testing.T) {},
			patternID: "nonexistent-pattern",
			projectID: "proj-a",
			taskType:  "feat",
			wantMin:   0.5,
			wantMax:   0.5,
		},
		{
			name: "100% success rate with fresh timestamp",
			setup: func(t *testing.T) {
				for i := 0; i < 5; i++ {
					if err := store.RecordPatternOutcome("fresh-success", "proj-a", "feat", "opus", true); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
			},
			patternID: "fresh-success",
			projectID: "proj-a",
			taskType:  "feat",
			// successRate=1.0, recency≈1.0 (just recorded) → ~1.0
			wantMin: 0.95,
			wantMax: 1.0,
		},
		{
			name: "mixed outcomes 60% success",
			setup: func(t *testing.T) {
				for i := 0; i < 3; i++ {
					if err := store.RecordPatternOutcome("mixed-pat", "proj-b", "fix", "sonnet", true); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
				for i := 0; i < 2; i++ {
					if err := store.RecordPatternOutcome("mixed-pat", "proj-b", "fix", "sonnet", false); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
			},
			patternID: "mixed-pat",
			projectID: "proj-b",
			taskType:  "fix",
			// successRate=0.6, recency≈1.0 → ~0.6
			wantMin: 0.55,
			wantMax: 0.65,
		},
		{
			name: "cross-project divergence — high success project",
			setup: func(t *testing.T) {
				for i := 0; i < 8; i++ {
					if err := store.RecordPatternOutcome("diverge-pat", "proj-high", "refactor", "opus", true); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
				for i := 0; i < 2; i++ {
					if err := store.RecordPatternOutcome("diverge-pat", "proj-high", "refactor", "opus", false); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
			},
			patternID: "diverge-pat",
			projectID: "proj-high",
			taskType:  "refactor",
			// successRate=0.8, recency≈1.0 → ~0.8
			wantMin: 0.75,
			wantMax: 0.85,
		},
		{
			name: "cross-project divergence — low success project",
			setup: func(t *testing.T) {
				for i := 0; i < 2; i++ {
					if err := store.RecordPatternOutcome("diverge-pat", "proj-low", "refactor", "opus", true); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
				for i := 0; i < 8; i++ {
					if err := store.RecordPatternOutcome("diverge-pat", "proj-low", "refactor", "opus", false); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
			},
			patternID: "diverge-pat",
			projectID: "proj-low",
			taskType:  "refactor",
			// successRate=0.2, recency≈1.0 → ~0.2
			wantMin: 0.15,
			wantMax: 0.25,
		},
		{
			name: "mixed task types — feat vs fix same pattern",
			setup: func(t *testing.T) {
				for i := 0; i < 5; i++ {
					if err := store.RecordPatternOutcome("tasktype-pat", "proj-c", "feat", "sonnet", true); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
				for i := 0; i < 5; i++ {
					if err := store.RecordPatternOutcome("tasktype-pat", "proj-c", "fix", "sonnet", false); err != nil {
						t.Fatalf("RecordPatternOutcome failed: %v", err)
					}
				}
			},
			patternID: "tasktype-pat",
			projectID: "proj-c",
			taskType:  "feat",
			// feat: 5 success, 0 fail → rate=1.0, recency≈1.0
			wantMin: 0.95,
			wantMax: 1.0,
		},
		{
			name: "UPSERT accumulates correctly",
			setup: func(t *testing.T) {
				// Record same combo multiple times
				if err := store.RecordPatternOutcome("upsert-pat", "proj-d", "test", "haiku", true); err != nil {
					t.Fatalf("RecordPatternOutcome failed: %v", err)
				}
				if err := store.RecordPatternOutcome("upsert-pat", "proj-d", "test", "haiku", true); err != nil {
					t.Fatalf("RecordPatternOutcome failed: %v", err)
				}
				if err := store.RecordPatternOutcome("upsert-pat", "proj-d", "test", "haiku", false); err != nil {
					t.Fatalf("RecordPatternOutcome failed: %v", err)
				}
			},
			patternID: "upsert-pat",
			projectID: "proj-d",
			taskType:  "test",
			// 2 success, 1 failure → rate=0.667, recency≈1.0
			wantMin: 0.6,
			wantMax: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			got := store.GetContextualConfidence(tt.patternID, tt.projectID, tt.taskType)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("GetContextualConfidence() = %f, want [%f, %f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestGetContextualConfidence_DecayedPattern(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pattern-perf-decay-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Insert a record with an old last_used timestamp (90 days ago)
	_, err = store.db.Exec(`
		INSERT INTO pattern_performance (pattern_id, project_id, task_type, model, success_count, failure_count, last_used)
		VALUES (?, ?, ?, ?, 10, 0, ?)
	`, "old-pattern", "proj-e", "feat", "opus", time.Now().AddDate(0, 0, -90).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("failed to insert old pattern: %v", err)
	}

	got := store.GetContextualConfidence("old-pattern", "proj-e", "feat")
	// successRate=1.0, recencyDecay = 1/(1+90/30) = 1/4 = 0.25
	// contextual = 1.0 * 0.25 = 0.25
	if got > 0.26 {
		t.Errorf("Decayed pattern confidence = %f, want <= 0.25", got)
	}
	if got < 0.20 {
		t.Errorf("Decayed pattern confidence = %f, want >= 0.20 (should be ~0.25)", got)
	}
}
