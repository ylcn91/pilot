package executor

import (
	"context"
	"testing"
)

// qaRoutingBackend records how many times it was invoked so a test can assert
// which backend runSelfReview routed through. Name() doubles as an identity tag.
type qaRoutingBackend struct {
	name  string
	calls int
}

func (b *qaRoutingBackend) Name() string      { return b.name }
func (b *qaRoutingBackend) IsAvailable() bool { return true }
func (b *qaRoutingBackend) Execute(_ context.Context, _ ExecuteOptions) (*BackendResult, error) {
	b.calls++
	return &BackendResult{Success: true, Output: "REVIEW_PASSED"}, nil
}

// TestSelfReviewRoutesThroughQABackend is the integration test for F1: with TDD
// enabled and a distinct tdd.qa backend, runSelfReview must invoke r.qaBackend
// (the QA role owns review), NOT r.reviewBackend. With TDD disabled it must keep
// using r.reviewBackend. Before the fix runSelfReview always used reviewBackend,
// so the TDD-enabled case here fails (qaBackend.calls == 0, reviewBackend.calls == 1).
func TestSelfReviewRoutesThroughQABackend(t *testing.T) {
	task := &Task{
		ID:          "F1-qa-routing",
		Title:       "Implement payment callback handler with retries",
		Description: "Add a handler that processes payment callbacks, records outcomes, and retries on transient failure.",
		ProjectPath: t.TempDir(),
	}

	newRunner := func(tddEnabled bool) (*Runner, *qaRoutingBackend, *qaRoutingBackend, *qaRoutingBackend) {
		execB := &qaRoutingBackend{name: BackendTypeCodexExec}
		reviewB := &qaRoutingBackend{name: BackendTypeClaudeCode}
		qaB := &qaRoutingBackend{name: BackendTypeOpenCode}

		r := NewRunnerWithBackend(execB)
		r.execBackend = execB
		r.reviewBackend = reviewB
		r.qaBackend = qaB
		r.config = &BackendConfig{
			TDD: &TDDConfig{
				Enabled: tddEnabled,
				QA:      &StageConfig{Type: BackendTypeOpenCode},
			},
		}
		return r, execB, reviewB, qaB
	}

	t.Run("TDD enabled routes self-review through qaBackend", func(t *testing.T) {
		r, execB, reviewB, qaB := newRunner(true)

		state := &progressState{}
		if err := r.runSelfReview(context.Background(), task, state); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}

		if qaB.calls != 1 {
			t.Errorf("qaBackend calls = %d, want 1 (TDD QA role owns self-review)", qaB.calls)
		}
		if reviewB.calls != 0 {
			t.Errorf("reviewBackend calls = %d, want 0 (must be bypassed in TDD mode)", reviewB.calls)
		}
		if execB.calls != 0 {
			t.Errorf("execBackend calls = %d, want 0 (execute backend must not run self-review)", execB.calls)
		}
	})

	t.Run("TDD disabled keeps using reviewBackend", func(t *testing.T) {
		r, execB, reviewB, qaB := newRunner(false)

		state := &progressState{}
		if err := r.runSelfReview(context.Background(), task, state); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}

		if reviewB.calls != 1 {
			t.Errorf("reviewBackend calls = %d, want 1 (non-TDD self-review path unchanged)", reviewB.calls)
		}
		if qaB.calls != 0 {
			t.Errorf("qaBackend calls = %d, want 0 (qaBackend must be unused when TDD disabled)", qaB.calls)
		}
		if execB.calls != 0 {
			t.Errorf("execBackend calls = %d, want 0", execB.calls)
		}
	})
}
