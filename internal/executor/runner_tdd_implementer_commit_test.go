package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFileNoCommit returns an action that, on each call, writes a file into the
// working tree WITHOUT committing it. It models an IMPLEMENTER backend that makes
// the tests pass in the working tree but never lands a commit — the exact gap the
// implementer-commit guard must catch.
func writeFileNoCommit(t *testing.T, dir, name, content string) func() {
	t.Helper()
	return func() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// TestRunTDDSequenceImplementerNoCommitFails is the integration proof for the
// gap: the IMPLEMENTER writes a passing implementation to the working tree but
// never commits. The GREEN gate (scripted to pass) is satisfied by the working
// tree, yet because no implementer commit lands beyond the test-author baseline
// the run must ABORT with reasonTDDImplementerNoCommit rather than finalizing a
// tests-only PR. Before the fix this run "succeeded" and returned the implementer
// result; after the fix it fails closed.
func TestRunTDDSequenceImplementerNoCommitFails(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	// RED gate: tests fail (red satisfied). GREEN gate: always pass — the working
	// tree the no-committing implementer wrote is "green", which is the whole trap.
	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})

	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	// Implementer makes tests pass in the working tree but NEVER commits, on every
	// invocation (initial + every re-prompt), so no commit ever lands.
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED (uncommitted)",
		action: writeFileNoCommit(t, dir, "add.go", "package tddtest\n"),
	}

	res, err := r.runTDDSequence(s)
	if err == nil || !strings.Contains(err.Error(), reasonTDDImplementerNoCommit) {
		t.Fatalf("expected %s abort, got res=%+v err=%v", reasonTDDImplementerNoCommit, res, err)
	}

	// The guard must have re-prompted the implementer (bounded by greenMax) before
	// giving up: at least the initial implementer call plus one re-prompt.
	implementerCalls := 0
	for _, role := range *order {
		if role == "implementer" {
			implementerCalls++
		}
	}
	if implementerCalls < 2 {
		t.Errorf("expected the implementer to be re-prompted to commit, got %d implementer calls in %v", implementerCalls, *order)
	}
}

// TestRunTDDSequenceImplementerCommitsRecovers verifies the re-prompt path: the
// implementer first writes WITHOUT committing (guard trips), then on the COMMIT
// re-prompt actually commits. GREEN stays green and the run SUCCEEDS — proving
// the guard fails closed only when no commit ever lands, not on a transient miss.
func TestRunTDDSequenceImplementerCommitsRecovers(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	// RED fail, then GREEN passes for the initial check and the post-commit re-check.
	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})

	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}

	// First implementer call: write but do NOT commit. The COMMIT re-prompt: commit.
	writeOnly := writeFileNoCommit(t, dir, "add.go", "package tddtest\n")
	commitImpl := commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add")
	calls := 0
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED",
		action: func() {
			calls++
			if calls == 1 {
				writeOnly()
				return
			}
			commitImpl()
		},
	}

	res, err := r.runTDDSequence(s)
	if err != nil {
		t.Fatalf("runTDDSequence (recover): %v", err)
	}
	if res == nil || res.Output != "IMPLEMENTED" {
		t.Fatalf("expected implementer result after commit recovery, got %+v", res)
	}
	if calls < 2 {
		t.Errorf("expected a COMMIT re-prompt after the uncommitted first pass, got %d implementer calls", calls)
	}
}
