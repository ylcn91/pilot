package executor

import (
	"context"
	"sync/atomic"
	"testing"
)

// optionRecordingBackend records the ExecuteOptions of the last Execute call so a
// test can assert which Model/Effort the runner threaded into the backend. It is
// the recording backend for the F3 per-stage override integration test.
type optionRecordingBackend struct {
	name      string
	calls     int
	lastModel string
	lastEffrt string
}

func (b *optionRecordingBackend) Name() string      { return b.name }
func (b *optionRecordingBackend) IsAvailable() bool { return true }
func (b *optionRecordingBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.calls++
	b.lastModel = opts.Model
	b.lastEffrt = opts.Effort
	return &BackendResult{Success: true, Output: "REVIEW_PASSED"}, nil
}

// substantialTask returns a task whose complexity is high enough that runSelfReview
// does not early-return on the trivial-task guard (ShouldSkipNavigator).
func substantialTask(id string) *Task {
	return &Task{
		ID:          id,
		Title:       "Implement payment callback handler with retries",
		Description: "Add a handler that processes payment callbacks, records outcomes, and retries on transient failure across multiple providers.",
		ProjectPath: "/tmp",
	}
}

// TestStageModelEffortOverrideReachesBackend is the F3 integration test: a
// per-stage Model/Effort override (pipeline.execute, pipeline.review / tdd.qa, and
// a TDD role) must reach the backend's ExecuteOptions, NOT be shadowed by the
// run-level s.selectedModel. Before the fix the execute, review/qa, and TDD-role
// Execute calls passed s.selectedModel verbatim, so each "stage override" subtest
// fails (got the run-level model instead of the stage model).
func TestStageModelEffortOverrideReachesBackend(t *testing.T) {
	const (
		runModel    = "run-model"
		runEffort   = "run-effort"
		execModel   = "stage-model-x"
		execEffort  = "stage-effort-x"
		roleModel   = "role-model-y"
		qaModel     = "qa-model-z"
		reviewModel = "review-model-w"
	)

	t.Run("execute stage override reaches execBackend", func(t *testing.T) {
		exec := &optionRecordingBackend{name: BackendTypeCodexExec}
		r := NewRunnerWithBackend(exec)
		r.execBackend = exec
		r.config = &BackendConfig{
			Pipeline: &PipelineConfig{
				Execute: &StageConfig{Type: BackendTypeCodexExec, Model: execModel, Effort: execEffort},
			},
		}
		s := &executeState{
			task:           substantialTask("F3-exec"),
			log:            r.log,
			state:          &progressState{},
			selectedModel:  runModel,
			selectedEffort: runEffort,
			prompt:         "do the thing",
			executionPath:  "/tmp",
		}
		var lastEventAt atomic.Int64
		if _, err := r.executePrimaryBackend(s, context.Background(), 0, 0, nil, "", &lastEventAt); err != nil {
			t.Fatalf("executePrimaryBackend: %v", err)
		}
		if exec.lastModel != execModel {
			t.Errorf("execBackend Model = %q, want stage override %q (NOT run-level %q)", exec.lastModel, execModel, runModel)
		}
		if exec.lastEffrt != execEffort {
			t.Errorf("execBackend Effort = %q, want stage override %q", exec.lastEffrt, execEffort)
		}
	})

	t.Run("execute falls back to selectedModel when stage sets no model", func(t *testing.T) {
		exec := &optionRecordingBackend{name: BackendTypeCodexExec}
		r := NewRunnerWithBackend(exec)
		r.execBackend = exec
		r.config = &BackendConfig{
			Pipeline: &PipelineConfig{Execute: &StageConfig{Type: BackendTypeCodexExec}},
		}
		s := &executeState{
			task:           substantialTask("F3-exec-fallback"),
			log:            r.log,
			state:          &progressState{},
			selectedModel:  runModel,
			selectedEffort: runEffort,
			prompt:         "do the thing",
			executionPath:  "/tmp",
		}
		var lastEventAt atomic.Int64
		if _, err := r.executePrimaryBackend(s, context.Background(), 0, 0, nil, "", &lastEventAt); err != nil {
			t.Fatalf("executePrimaryBackend: %v", err)
		}
		if exec.lastModel != runModel {
			t.Errorf("execBackend Model = %q, want run-level fallback %q", exec.lastModel, runModel)
		}
		if exec.lastEffrt != runEffort {
			t.Errorf("execBackend Effort = %q, want run-level fallback %q", exec.lastEffrt, runEffort)
		}
	})

	t.Run("TDD implementer role override reaches role backend", func(t *testing.T) {
		impl := &optionRecordingBackend{name: BackendTypeClaudeCode}
		r := NewRunnerWithBackend(impl)
		r.implementerBackend = impl
		r.config = &BackendConfig{
			TDD: &TDDConfig{
				Enabled:     true,
				Implementer: &StageConfig{Type: BackendTypeClaudeCode, Model: roleModel},
			},
		}
		s := &executeState{
			task:           substantialTask("F3-role"),
			ctx:            context.Background(),
			log:            r.log,
			state:          &progressState{},
			selectedModel:  runModel,
			selectedEffort: runEffort,
			executionPath:  "/tmp",
		}
		if _, err := r.runTDDRole(s, r.implementerBackend, r.tddRoleStage("implementer"), "prompt"); err != nil {
			t.Fatalf("runTDDRole: %v", err)
		}
		if impl.lastModel != roleModel {
			t.Errorf("implementer Model = %q, want role override %q (NOT run-level %q)", impl.lastModel, roleModel, runModel)
		}
	})

	t.Run("TDD role falls back to selectedModel when role sets no model", func(t *testing.T) {
		impl := &optionRecordingBackend{name: BackendTypeClaudeCode}
		r := NewRunnerWithBackend(impl)
		r.implementerBackend = impl
		r.config = &BackendConfig{
			TDD: &TDDConfig{
				Enabled:     true,
				Implementer: &StageConfig{Type: BackendTypeClaudeCode},
			},
		}
		s := &executeState{
			task:           substantialTask("F3-role-fallback"),
			ctx:            context.Background(),
			log:            r.log,
			state:          &progressState{},
			selectedModel:  runModel,
			selectedEffort: runEffort,
			executionPath:  "/tmp",
		}
		if _, err := r.runTDDRole(s, r.implementerBackend, r.tddRoleStage("implementer"), "prompt"); err != nil {
			t.Fatalf("runTDDRole: %v", err)
		}
		if impl.lastModel != runModel {
			t.Errorf("implementer Model = %q, want run-level fallback %q", impl.lastModel, runModel)
		}
	})

	t.Run("TDD QA stage override reaches self-review backend", func(t *testing.T) {
		qa := &optionRecordingBackend{name: BackendTypeOpenCode}
		r := NewRunnerWithBackend(qa)
		r.qaBackend = qa
		// DefaultModel drives the run-level resolveSelectedModel for non-CC backends.
		r.config = &BackendConfig{
			Type:         BackendTypeCodexExec,
			DefaultModel: runModel,
			TDD: &TDDConfig{
				Enabled: true,
				QA:      &StageConfig{Type: BackendTypeOpenCode, Model: qaModel},
			},
		}
		if err := r.runSelfReview(context.Background(), substantialTask("F3-qa"), &progressState{}); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}
		if qa.calls != 1 {
			t.Fatalf("qaBackend calls = %d, want 1", qa.calls)
		}
		if qa.lastModel != qaModel {
			t.Errorf("qaBackend Model = %q, want tdd.qa override %q (NOT run-level %q)", qa.lastModel, qaModel, runModel)
		}
	})

	t.Run("pipeline review stage override reaches self-review backend", func(t *testing.T) {
		review := &optionRecordingBackend{name: BackendTypeClaudeCode}
		r := NewRunnerWithBackend(review)
		r.reviewBackend = review
		r.config = &BackendConfig{
			Type:         BackendTypeCodexExec,
			DefaultModel: runModel,
			Pipeline: &PipelineConfig{
				Review: &StageConfig{Type: BackendTypeClaudeCode, Model: reviewModel},
			},
		}
		if err := r.runSelfReview(context.Background(), substantialTask("F3-review"), &progressState{}); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}
		if review.calls != 1 {
			t.Fatalf("reviewBackend calls = %d, want 1", review.calls)
		}
		if review.lastModel != reviewModel {
			t.Errorf("reviewBackend Model = %q, want pipeline.review override %q (NOT run-level %q)", review.lastModel, reviewModel, runModel)
		}
	})

	t.Run("self-review falls back to selectedModel when no stage override", func(t *testing.T) {
		review := &optionRecordingBackend{name: BackendTypeClaudeCode}
		r := NewRunnerWithBackend(review)
		r.reviewBackend = review
		r.config = &BackendConfig{
			Type:         BackendTypeCodexExec,
			DefaultModel: runModel,
		}
		task := substantialTask("F3-review-fallback")
		// With no pipeline.review / tdd.qa override, self-review must use the same
		// run-level routing as the main execution (resolveSelectedModel).
		wantRunLevel := r.resolveSelectedModel(task)
		if err := r.runSelfReview(context.Background(), task, &progressState{}); err != nil {
			t.Fatalf("runSelfReview: %v", err)
		}
		if review.calls != 1 {
			t.Fatalf("reviewBackend calls = %d, want 1", review.calls)
		}
		if review.lastModel != wantRunLevel {
			t.Errorf("reviewBackend Model = %q, want run-level fallback %q", review.lastModel, wantRunLevel)
		}
	})
}
