package executor

import (
	"context"
	"testing"
)

// resumeRecordingBackend records the ResumeSessionID of every Execute call so a
// test can assert whether the self-review path forwarded a session id.
type resumeRecordingBackend struct {
	name      string
	resumeIDs []string
}

func (b *resumeRecordingBackend) Name() string      { return b.name }
func (b *resumeRecordingBackend) IsAvailable() bool { return true }

func (b *resumeRecordingBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.resumeIDs = append(b.resumeIDs, opts.ResumeSessionID)
	return &BackendResult{Success: true, Output: "REVIEW_PASSED"}, nil
}

// TestSelfReviewResumeAcrossBackends verifies the cross-backend resume edge:
// when review runs on the SAME backend instance as execute, the self-review
// call forwards the captured session id (existing GH-1265 behavior). When a
// pipeline routes review to a DIFFERENT backend than execute, no ResumeSessionID
// is forwarded — a Codex session id is meaningless to a claude-code reviewer.
func TestSelfReviewResumeAcrossBackends(t *testing.T) {
	const sessionID = "session-abc"

	task := &Task{
		ID:          "GH-handoff",
		Title:       "Implement payment handler",
		Description: "Add a handler that processes payment callbacks and records outcomes.",
		ProjectPath: t.TempDir(),
	}

	newRunner := func(execB, reviewB Backend) *Runner {
		r := NewRunnerWithBackend(execB)
		r.execBackend = execB
		r.reviewBackend = reviewB
		// GH-1265 resume is gated on this flag; enable so the only remaining
		// gate is the same-instance check we are exercising.
		r.config = &BackendConfig{ClaudeCode: &ClaudeCodeConfig{UseSessionResume: true}}
		return r
	}

	t.Run("single backend forwards session id", func(t *testing.T) {
		backend := &resumeRecordingBackend{name: BackendTypeClaudeCode}
		r := newRunner(backend, backend) // review == execute (same pointer)

		state := &progressState{sessionID: sessionID}
		if err := r.runSelfReview(context.Background(), task, state); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}
		if len(backend.resumeIDs) != 1 {
			t.Fatalf("expected 1 review execute call, got %d", len(backend.resumeIDs))
		}
		if backend.resumeIDs[0] != sessionID {
			t.Errorf("ResumeSessionID = %q, want %q (same-backend resume must be forwarded)", backend.resumeIDs[0], sessionID)
		}
	})

	t.Run("cross-backend review drops session id", func(t *testing.T) {
		execB := &resumeRecordingBackend{name: BackendTypeCodexExec}
		reviewB := &resumeRecordingBackend{name: BackendTypeClaudeCode}
		r := newRunner(execB, reviewB) // review != execute (distinct pointers)

		state := &progressState{sessionID: sessionID}
		if err := r.runSelfReview(context.Background(), task, state); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}
		if len(reviewB.resumeIDs) != 1 {
			t.Fatalf("expected 1 review execute call on review backend, got %d", len(reviewB.resumeIDs))
		}
		if reviewB.resumeIDs[0] != "" {
			t.Errorf("ResumeSessionID = %q, want empty (cross-backend resume is invalid)", reviewB.resumeIDs[0])
		}
		if len(execB.resumeIDs) != 0 {
			t.Errorf("execute backend must not run self-review, got %d calls", len(execB.resumeIDs))
		}
	})
}

// TestResolveStageBackends verifies that with no pipeline configured all three
// stage backends are the identical single-backend instance (zero behavior
// change), and that a configured stage gets its own backend while the others
// fall back to the single backend.
func TestResolveStageBackends(t *testing.T) {
	tests := []struct {
		name string
		// config drives runner construction. Type is the run's primary backend.
		config *BackendConfig
		// wantPlanName/wantExecName/wantReviewName are the expected Name() per stage.
		wantPlanName   string
		wantExecName   string
		wantReviewName string
		// wantAllSingle asserts every stage points at the same single backend pointer.
		wantAllSingle bool
	}{
		{
			name:           "nil pipeline falls back to single backend",
			config:         &BackendConfig{Type: BackendTypeCodexExec},
			wantPlanName:   BackendTypeCodexExec,
			wantExecName:   BackendTypeCodexExec,
			wantReviewName: BackendTypeCodexExec,
			wantAllSingle:  true,
		},
		{
			name: "execute stage overrides only the exec backend",
			config: &BackendConfig{
				Type:     BackendTypeCodexExec,
				Pipeline: &PipelineConfig{Execute: &StageConfig{Type: BackendTypeOpenCode}},
			},
			wantPlanName:   BackendTypeCodexExec,
			wantExecName:   BackendTypeOpenCode,
			wantReviewName: BackendTypeCodexExec,
			wantAllSingle:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRunnerWithConfig(tt.config)
			if err != nil {
				t.Fatalf("NewRunnerWithConfig: %v", err)
			}
			if got := r.planBackend.Name(); got != tt.wantPlanName {
				t.Errorf("planBackend = %q, want %q", got, tt.wantPlanName)
			}
			if got := r.execBackend.Name(); got != tt.wantExecName {
				t.Errorf("execBackend = %q, want %q", got, tt.wantExecName)
			}
			if got := r.reviewBackend.Name(); got != tt.wantReviewName {
				t.Errorf("reviewBackend = %q, want %q", got, tt.wantReviewName)
			}
			if tt.wantAllSingle {
				if r.planBackend != r.backend || r.execBackend != r.backend || r.reviewBackend != r.backend {
					t.Error("with no pipeline all stage backends must be the single backend instance")
				}
			} else {
				// Stages without an override still reuse the single backend pointer.
				if r.planBackend != r.backend || r.reviewBackend != r.backend {
					t.Error("unconfigured stages must fall back to the single backend instance")
				}
				if r.execBackend == r.backend {
					t.Error("configured execute stage must be a distinct backend instance")
				}
			}
		})
	}
}
