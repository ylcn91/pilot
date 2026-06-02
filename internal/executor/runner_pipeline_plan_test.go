package executor

import (
	"context"
	"strings"
	"testing"
)

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
