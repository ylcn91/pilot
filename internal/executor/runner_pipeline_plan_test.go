package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// recordingPlanBackend captures the ExecuteOptions of each Execute call so tests
// can assert the pipeline plan stage drives the configured backend with a
// read-only tool set. It returns a fixed spec as Output.
type recordingPlanBackend struct {
	output   string
	err      error
	calls    int
	lastOpts ExecuteOptions
}

func (b *recordingPlanBackend) Name() string      { return "recording-plan" }
func (b *recordingPlanBackend) IsAvailable() bool { return true }
func (b *recordingPlanBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.calls++
	b.lastOpts = opts
	if b.err != nil {
		return nil, b.err
	}
	return &BackendResult{Success: true, Output: b.output}, nil
}

// TestExecutePipelinePlan_InjectsSpec verifies the opt-in pipeline plan stage:
// when config.Pipeline.Plan is set it captures a spec into s.planOutput, and the
// assembled execute prompt then carries an "## Implementation Plan" section.
func TestExecutePipelinePlan_InjectsSpec(t *testing.T) {
	const spec = "1. **feat(api): add handler** - wire the route"

	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeClaudeCode}}
	r.planPipelineFn = func() (string, error) { return spec, nil }

	s := &executeState{task: &Task{ID: "GH-1", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if s.planOutput != spec {
		t.Fatalf("planOutput = %q, want %q", s.planOutput, spec)
	}

	prompt := injectPlanOutput("base prompt", s.planOutput)
	if !strings.Contains(prompt, "## Implementation Plan") {
		t.Fatalf("prompt missing plan section:\n%s", prompt)
	}
	if !strings.Contains(prompt, spec) {
		t.Fatalf("prompt missing spec body:\n%s", prompt)
	}
}

// TestExecutePipelinePlan_BuildsTypedArtifact verifies the plan stage stores a
// typed "plan" handoff artifact alongside the prose injection: role plan, the
// raw spec as content, a chain-root ParentHash (empty), the task ID, the current
// schema version, and the deterministic TraceHash over those fields.
func TestExecutePipelinePlan_BuildsTypedArtifact(t *testing.T) {
	const spec = "1. **feat(api): add handler** - wire the route"
	const taskID = "GH-art-1"

	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeClaudeCode}}
	r.planPipelineFn = func() (string, error) { return spec, nil }

	s := &executeState{task: &Task{ID: taskID, Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	art := s.planArtifact
	if art.Role != pilotapi.RolePlan {
		t.Errorf("artifact role = %q, want %q", art.Role, pilotapi.RolePlan)
	}
	if art.Content != spec {
		t.Errorf("artifact content = %q, want spec %q", art.Content, spec)
	}
	if art.ParentHash != "" {
		t.Errorf("plan artifact ParentHash = %q, want empty (chain root)", art.ParentHash)
	}
	if art.TaskID != taskID {
		t.Errorf("artifact TaskID = %q, want %q", art.TaskID, taskID)
	}
	if art.SchemaVersion != pilotapi.SchemaVersion {
		t.Errorf("artifact SchemaVersion = %d, want %d", art.SchemaVersion, pilotapi.SchemaVersion)
	}
	want := pilotapi.TraceHash(pilotapi.RolePlan, taskID, spec, "")
	if art.TraceHash != want {
		t.Errorf("artifact TraceHash = %q, want %q", art.TraceHash, want)
	}
	// The typed artifact equals a freshly built one (deterministic constructor).
	if art != pilotapi.NewHandoffArtifact(pilotapi.RolePlan, taskID, spec, "") {
		t.Errorf("plan artifact not equal to NewHandoffArtifact(...): %+v", art)
	}
}

// TestExecutePipelinePlan_NoArtifactWhenSkipped verifies that with no plan stage
// configured the artifact stays the zero value (no TraceHash), matching the
// no-op planOutput behavior — no traceable record is fabricated.
func TestExecutePipelinePlan_NoArtifactWhenSkipped(t *testing.T) {
	r := newTestRunner("claude")
	s := &executeState{task: &Task{ID: "GH-art-2", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if s.planArtifact != (pilotapi.HandoffArtifact{}) {
		t.Fatalf("planArtifact = %+v, want zero value when stage skipped", s.planArtifact)
	}
	if s.planArtifact.TraceHash != "" {
		t.Fatalf("planArtifact.TraceHash = %q, want empty when stage skipped", s.planArtifact.TraceHash)
	}
}

// TestExecutePipelinePlan_NoArtifactOnFailure verifies a plan-stage error leaves
// the typed artifact at its zero value (no spurious traceable record) just as it
// leaves planOutput empty.
func TestExecutePipelinePlan_NoArtifactOnFailure(t *testing.T) {
	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeClaudeCode}}
	r.planPipelineFn = func() (string, error) { return "", context.DeadlineExceeded }

	s := &executeState{task: &Task{ID: "GH-art-3", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if s.planArtifact != (pilotapi.HandoffArtifact{}) {
		t.Fatalf("planArtifact = %+v, want zero value after plan failure", s.planArtifact)
	}
}

// TestExecutePipelinePlan_NoConfig verifies the stage is a strict no-op when no
// pipeline plan is configured: planOutput stays empty, the assembled prompt has
// no plan section, and the injected planPipelineFn is never called (so no LLM
// round-trip is added to a normal run).
func TestExecutePipelinePlan_NoConfig(t *testing.T) {
	r := newTestRunner("claude")
	called := false
	r.planPipelineFn = func() (string, error) {
		called = true
		return "should not be used", nil
	}

	s := &executeState{task: &Task{ID: "GH-2", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if called {
		t.Fatal("planPipelineFn called without config.Pipeline.Plan set")
	}
	if s.planOutput != "" {
		t.Fatalf("planOutput = %q, want empty", s.planOutput)
	}

	prompt := injectPlanOutput("base prompt", s.planOutput)
	if strings.Contains(prompt, "## Implementation Plan") {
		t.Fatalf("prompt unexpectedly contains plan section:\n%s", prompt)
	}
	if prompt != "base prompt" {
		t.Fatalf("prompt mutated without a spec: %q", prompt)
	}
}

// TestExecutePipelinePlan_FailureNonFatal verifies a plan-stage error is
// swallowed (logged, not propagated) and leaves planOutput empty so execution
// proceeds without a spec rather than aborting.
func TestExecutePipelinePlan_FailureNonFatal(t *testing.T) {
	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeClaudeCode}}
	r.planPipelineFn = func() (string, error) { return "", context.DeadlineExceeded }

	s := &executeState{task: &Task{ID: "GH-3", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if s.planOutput != "" {
		t.Fatalf("planOutput = %q, want empty after failure", s.planOutput)
	}
}

// TestExecutePipelinePlan_RunsOnConfiguredBackend verifies the any-to-any plan
// path: with a configured plan stage and no test override, the stage drives the
// resolved planBackend (not the claude `--print` subprocess), passing the
// read-only tool set, and the backend's Output becomes the injected
// "## Implementation Plan" section.
func TestExecutePipelinePlan_RunsOnConfiguredBackend(t *testing.T) {
	const spec = "1. **feat(api): add handler** - design the route"

	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeCodexExec, Model: "gpt-5"}}
	plan := &recordingPlanBackend{output: spec}
	r.planBackend = plan

	s := &executeState{task: &Task{ID: "GH-4", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if plan.calls != 1 {
		t.Fatalf("planBackend.Execute calls = %d, want 1", plan.calls)
	}
	if got, want := strings.Join(plan.lastOpts.AllowedTools, ","), strings.Join(DefaultAllowedToolsPlanning(), ","); got != want {
		t.Fatalf("AllowedTools = %q, want read-only %q", got, want)
	}
	if plan.lastOpts.Model != "gpt-5" {
		t.Fatalf("Model = %q, want plan-stage model %q", plan.lastOpts.Model, "gpt-5")
	}
	if !strings.Contains(plan.lastOpts.Prompt, "spec ONLY") {
		t.Fatalf("prompt missing read-only contract:\n%s", plan.lastOpts.Prompt)
	}
	if s.planOutput != spec {
		t.Fatalf("planOutput = %q, want %q", s.planOutput, spec)
	}

	prompt := injectPlanOutput("base prompt", s.planOutput)
	if !strings.Contains(prompt, "## Implementation Plan") || !strings.Contains(prompt, spec) {
		t.Fatalf("assembled prompt missing plan section/spec:\n%s", prompt)
	}
}

// TestExecutePipelinePlan_NoConfigSkipsBackend verifies the configured plan
// backend is NOT invoked when no pipeline plan stage is set — no extra LLM
// round-trip is added to a normal run.
func TestExecutePipelinePlan_NoConfigSkipsBackend(t *testing.T) {
	r := newTestRunner("claude")
	plan := &recordingPlanBackend{output: "should not be used"}
	r.planBackend = plan

	s := &executeState{task: &Task{ID: "GH-5", Title: "do work"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if plan.calls != 0 {
		t.Fatalf("planBackend.Execute called %d times without Pipeline.Plan", plan.calls)
	}
	if s.planOutput != "" {
		t.Fatalf("planOutput = %q, want empty", s.planOutput)
	}
}
