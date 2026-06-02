package memory

import (
	"os"
	"testing"
)

func TestPatternFeedback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-feedback-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create a pattern
	pattern := &CrossPattern{
		ID:         "feedback_test_pattern",
		Type:       "code",
		Title:      "Error handling",
		Confidence: 0.6,
		Scope:      "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern failed: %v", err)
	}

	// Link to project
	if err := store.LinkPatternToProject("feedback_test_pattern", "/test/project"); err != nil {
		t.Fatalf("LinkPatternToProject failed: %v", err)
	}

	// Create execution
	exec := &Execution{
		ID:          "exec_1",
		TaskID:      "task_1",
		ProjectPath: "/test/project",
		Status:      "completed",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution failed: %v", err)
	}

	// Record positive feedback
	feedback := &PatternFeedback{
		PatternID:       "feedback_test_pattern",
		ExecutionID:     "exec_1",
		ProjectPath:     "/test/project",
		Outcome:         "success",
		ConfidenceDelta: 0.1,
	}
	if err := store.RecordPatternFeedback(feedback); err != nil {
		t.Fatalf("RecordPatternFeedback failed: %v", err)
	}

	// Check confidence increased
	updated, err := store.GetCrossPattern("feedback_test_pattern")
	if err != nil {
		t.Fatalf("GetCrossPattern failed: %v", err)
	}
	if updated.Confidence <= 0.6 {
		t.Errorf("expected confidence to increase, got %f", updated.Confidence)
	}
}

func TestPatternFeedbackConfidenceClamping(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-clamping-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create execution for feedback
	exec := &Execution{
		ID:          "clamp_exec_1",
		TaskID:      "clamp_task_1",
		ProjectPath: "/test/project",
		Status:      "completed",
	}
	if err := store.SaveExecution(exec); err != nil {
		t.Fatalf("SaveExecution failed: %v", err)
	}

	tests := []struct {
		name              string
		initialConfidence float64
		outcome           string
		delta             float64
		expectedMin       float64
		expectedMax       float64
	}{
		{
			name:              "success should not exceed upper bound 0.95",
			initialConfidence: 0.90,
			outcome:           "success",
			delta:             0.20,
			expectedMin:       0.95,
			expectedMax:       0.95,
		},
		{
			name:              "failure should not go below lower bound 0.1",
			initialConfidence: 0.15,
			outcome:           "failure",
			delta:             0.20,
			expectedMin:       0.10,
			expectedMax:       0.10,
		},
		{
			name:              "success within bounds",
			initialConfidence: 0.50,
			outcome:           "success",
			delta:             0.10,
			expectedMin:       0.59,
			expectedMax:       0.61,
		},
		{
			name:              "failure within bounds",
			initialConfidence: 0.50,
			outcome:           "failure",
			delta:             0.10,
			expectedMin:       0.39,
			expectedMax:       0.41,
		},
		{
			name:              "success at max boundary stays at max",
			initialConfidence: 0.95,
			outcome:           "success",
			delta:             0.10,
			expectedMin:       0.95,
			expectedMax:       0.95,
		},
		{
			name:              "failure at min boundary stays at min",
			initialConfidence: 0.10,
			outcome:           "failure",
			delta:             0.10,
			expectedMin:       0.10,
			expectedMax:       0.10,
		},
		{
			name:              "large success delta clamps to max",
			initialConfidence: 0.50,
			outcome:           "success",
			delta:             1.00,
			expectedMin:       0.95,
			expectedMax:       0.95,
		},
		{
			name:              "large failure delta clamps to min",
			initialConfidence: 0.50,
			outcome:           "failure",
			delta:             1.00,
			expectedMin:       0.10,
			expectedMax:       0.10,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			patternID := "clamp_pattern_" + string(rune('a'+i))

			// Create pattern with initial confidence
			pattern := &CrossPattern{
				ID:         patternID,
				Type:       "code",
				Title:      tc.name,
				Confidence: tc.initialConfidence,
				Scope:      "org",
			}
			if err := store.SaveCrossPattern(pattern); err != nil {
				t.Fatalf("SaveCrossPattern failed: %v", err)
			}

			// Link to project
			if err := store.LinkPatternToProject(patternID, "/test/project"); err != nil {
				t.Fatalf("LinkPatternToProject failed: %v", err)
			}

			// Record feedback
			feedback := &PatternFeedback{
				PatternID:       patternID,
				ExecutionID:     "clamp_exec_1",
				ProjectPath:     "/test/project",
				Outcome:         tc.outcome,
				ConfidenceDelta: tc.delta,
			}
			if err := store.RecordPatternFeedback(feedback); err != nil {
				t.Fatalf("RecordPatternFeedback failed: %v", err)
			}

			// Verify confidence is within expected bounds
			updated, err := store.GetCrossPattern(patternID)
			if err != nil {
				t.Fatalf("GetCrossPattern failed: %v", err)
			}

			if updated.Confidence < tc.expectedMin || updated.Confidence > tc.expectedMax {
				t.Errorf("confidence %f not in expected range [%f, %f]",
					updated.Confidence, tc.expectedMin, tc.expectedMax)
			}
		})
	}
}
