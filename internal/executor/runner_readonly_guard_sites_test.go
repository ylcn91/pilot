package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// committingPlanBackend is a plan Backend whose Execute commits a file in the
// repo before returning a spec — i.e. a misbehaving non-claude planner that
// ignores the read-only contract and pollutes the branch.
type committingPlanBackend struct {
	t      *testing.T
	dir    string
	output string
	calls  int
}

func (b *committingPlanBackend) Name() string      { return "committing-plan" }
func (b *committingPlanBackend) IsAvailable() bool { return true }
func (b *committingPlanBackend) Execute(_ context.Context, _ ExecuteOptions) (*BackendResult, error) {
	b.calls++
	commitNamed(b.t, b.dir, "plan_stray.go", "package x\n", "plan: stray commit (should be reverted)")
	return &BackendResult{Success: true, Output: b.output}, nil
}

// TestExecutePipelinePlan_RevertsStrayCommit wires the real plan stage with a
// planBackend that commits. The read-only guard must revert that commit (HEAD
// back to pre-plan) and flag s.planReadOnlyViolation, while the spec is still
// captured into planOutput so the run proceeds.
func TestExecutePipelinePlan_RevertsStrayCommit(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	const spec = "1. design the route"

	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeCodexExec}}
	r.planBackend = &committingPlanBackend{t: t, dir: dir, output: spec}

	s := &executeState{
		task:          &Task{ID: "GH-ro-plan", Title: "do work"},
		ctx:           context.Background(),
		executionPath: dir,
		git:           git,
	}
	r.executePipelinePlan(s)

	if !s.planReadOnlyViolation {
		t.Error("planReadOnlyViolation = false, want true after planner committed")
	}
	nowHead, _ := git.GetCurrentCommitSHA(context.Background())
	if nowHead != head {
		t.Errorf("HEAD = %q after plan revert, want pre-plan %q", nowHead, head)
	}
	if s.planOutput != spec {
		t.Errorf("planOutput = %q, want spec captured despite revert", s.planOutput)
	}
	// Pristine restore: the planner's committed file is discarded so it cannot
	// leak into the execute diff.
	if _, err := os.Stat(filepath.Join(dir, "plan_stray.go")); !os.IsNotExist(err) {
		t.Errorf("pristine restore left planner file behind (err=%v)", err)
	}
	if dirty, _ := git.IsDirty(context.Background()); dirty {
		t.Error("worktree dirty after plan revert, want clean")
	}
}

// TestExecutePipelinePlan_WellBehavedNoViolation verifies a planner that does NOT
// commit leaves planReadOnlyViolation false and HEAD unchanged.
func TestExecutePipelinePlan_WellBehavedNoViolation(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)

	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeCodexExec}}
	r.planBackend = &recordingPlanBackend{output: "spec only, no commit"}

	s := &executeState{
		task:          &Task{ID: "GH-ro-plan-ok", Title: "do work"},
		ctx:           context.Background(),
		executionPath: dir,
		git:           git,
	}
	r.executePipelinePlan(s)

	if s.planReadOnlyViolation {
		t.Error("planReadOnlyViolation = true, want false for a well-behaved planner")
	}
	nowHead, _ := git.GetCurrentCommitSHA(context.Background())
	if nowHead != head {
		t.Errorf("HEAD moved on a well-behaved plan: %q != %q", nowHead, head)
	}
	if s.planOutput != "spec only, no commit" {
		t.Errorf("planOutput = %q, want captured spec", s.planOutput)
	}
}

// TestExecutePipelinePlan_NoGitGuardNoop verifies the plan stage runs without a
// git layer (s.git == nil): the guard is a no-op, no panic, no violation.
func TestExecutePipelinePlan_NoGitGuardNoop(t *testing.T) {
	r := newTestRunner("claude")
	r.config.Pipeline = &PipelineConfig{Plan: &StageConfig{Type: BackendTypeCodexExec}}
	r.planBackend = &recordingPlanBackend{output: "spec"}

	s := &executeState{task: &Task{ID: "GH-ro-plan-nogit"}, ctx: context.Background()}
	r.executePipelinePlan(s)

	if s.planReadOnlyViolation {
		t.Error("planReadOnlyViolation = true with nil git, want false")
	}
	if s.planOutput != "spec" {
		t.Errorf("planOutput = %q, want spec", s.planOutput)
	}
}

// TestRunTDDSequence_ArchitectStrayCommitReverted verifies the ARCHITECT guard:
// a design-only architect that commits is reverted to pre-architect HEAD BEFORE
// TEST-AUTHOR runs, and s.tddArchitectReadOnlyViolation is set. The sequence
// still completes through GREEN using the test-author/implementer commits.
func TestRunTDDSequence_ArchitectStrayCommitReverted(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})

	git := NewGitOperations(dir)
	headBeforeArch, _ := git.GetCurrentCommitSHA(context.Background())

	// Architect commits a stray file — it must be reverted before test-author.
	r.architectBackend = &tddRoleBackend{
		name: "arch", prompts: order, role: "architect", output: "DESIGN",
		action: func() { commitNamed(t, dir, "arch_stray.go", "package tddtest\n", "design: stray architect commit") },
	}
	// Captures HEAD at the moment test-author starts, to assert the architect's
	// commit was already gone (reverted) by then.
	var headAtTestAuthor string
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: func() {
			headAtTestAuthor, _ = git.GetCurrentCommitSHA(context.Background())
			commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test")()
		},
	}
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer", output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	res, err := r.runTDDSequence(s)
	if err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}
	if res == nil || res.Output != "IMPLEMENTED" {
		t.Fatalf("expected implementer result, got %+v", res)
	}

	if !s.tddArchitectReadOnlyViolation {
		t.Error("tddArchitectReadOnlyViolation = false, want true after architect committed")
	}
	if headAtTestAuthor != headBeforeArch {
		t.Errorf("HEAD at test-author start = %q, want pre-architect %q (architect commit not reverted)",
			headAtTestAuthor, headBeforeArch)
	}
	// Pristine restore: the architect's file is discarded so the test-author sees
	// a clean tree (the RED-gate integrity guarantee). The live architect-leak we
	// observed (calc.go left behind) is exactly what this asserts is gone.
	if _, err := os.Stat(filepath.Join(dir, "arch_stray.go")); !os.IsNotExist(err) {
		t.Errorf("pristine restore left architect file behind (err=%v)", err)
	}
}

// TestRunTDDSequence_ArchitectWellBehavedNoViolation verifies a design-only
// architect that does NOT commit leaves tddArchitectReadOnlyViolation false.
func TestRunTDDSequence_ArchitectWellBehavedNoViolation(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})

	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect", output: "DESIGN"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer", output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	if _, err := r.runTDDSequence(s); err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}
	if s.tddArchitectReadOnlyViolation {
		t.Error("tddArchitectReadOnlyViolation = true, want false for a well-behaved architect")
	}
	if s.tddArchitectDesign != "DESIGN" {
		t.Errorf("architect design = %q, want captured advisory output", s.tddArchitectDesign)
	}
}
