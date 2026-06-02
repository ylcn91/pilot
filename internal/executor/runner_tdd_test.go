package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tddRoleBackend is a mock Backend that records the prompt of each Execute call
// and runs an optional per-call action (e.g. committing a file) so the TDD
// sequence's CountNewCommits checks observe real repository changes.
type tddRoleBackend struct {
	name    string
	prompts *[]string // shared across the four role backends to capture call order
	role    string    // label appended to the shared order log
	output  string    // BackendResult.Output (e.g. the TESTS_ADDED line)
	action  func()    // optional side effect (commit a file) run on Execute
	err     error
}

func (b *tddRoleBackend) Name() string      { return b.name }
func (b *tddRoleBackend) IsAvailable() bool { return true }

func (b *tddRoleBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	*b.prompts = append(*b.prompts, b.role)
	if b.action != nil {
		b.action()
	}
	if b.err != nil {
		return nil, b.err
	}
	return &BackendResult{Success: true, Output: b.output}, nil
}

// tddGitRepo creates a temp git repo with a go.mod, an initial commit on the
// default branch, then a feature branch checked out. It returns the dir and the
// default branch name (the TDD commit-count base).
func tddGitRepo(t *testing.T) (dir, defaultBranch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir = t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tddtest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	git := NewGitOperations(dir)
	defaultBranch, _ = git.GetCurrentBranch(ctx)
	run("checkout", "-b", "pilot/GH-tdd")
	return dir, defaultBranch
}

// commitFile returns an action that, on each call, writes a uniquely-numbered
// file and commits it in dir, so repeated invocations (role retries) each land a
// fresh commit and CountNewCommits advances every time.
func commitFile(t *testing.T, dir, name, content, msg string) func() {
	n := 0
	return func() {
		n++
		ctx := context.Background()
		fname := fmt.Sprintf("%d_%s", n, name)
		if err := os.WriteFile(filepath.Join(dir, fname), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", fname, err)
		}
		for _, args := range [][]string{{"add", "."}, {"commit", "-m", msg}} {
			cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
}

// newTDDRunner wires a Runner with the four role backends and a scripted gate
// factory, plus an executeState pointing at the given repo.
func newTDDRunner(t *testing.T, dir, defaultBranch string, order *[]string, gateOutcomes []*QualityOutcome) (*Runner, *executeState, *[]string) {
	t.Helper()
	r := NewRunner()
	r.config = &BackendConfig{TDD: &TDDConfig{Enabled: true}}

	var gateCalls int
	gateCmds := &[]string{}
	r.SetTDDGateCheckerFactory(scriptedFactory(gateOutcomes, &gateCalls, gateCmds))
	// The Go per-test path bypasses the exit-code QualityChecker, so drive the
	// same scripted RED/GREEN outcomes through the injectable go-test runner. The
	// baseline call (nil testNames) always reports an empty green suite.
	r.tddGoTestRunner = scriptedGoTestRunner(gateOutcomes, &gateCalls, gateCmds)

	task := &Task{
		ID:          "GH-tdd",
		Title:       "Add adder",
		Description: "Implement an Add function and test it.",
		ProjectPath: dir,
		BaseBranch:  defaultBranch,
	}
	s := &executeState{
		task:          task,
		ctx:           context.Background(),
		executionPath: dir,
		log:           r.log,
		git:           NewGitOperations(dir),
		agentPath:     filepath.Join(dir, ".agent"),
		prompt:        "BASE PROMPT",
		state:         &progressState{},
	}
	return r, s, gateCmds
}

// TestRunTDDSequenceHappyPath exercises the full role sequence: architect ->
// test-author (commits a failing test) -> RED ok -> implementer (commits) ->
// GREEN ok -> returns the implementer result for the finalize tail.
func TestRunTDDSequenceHappyPath(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	// RED gate: tests fail (not passed => red satisfied). GREEN gate: pass.
	r, s, gateCmds := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})

	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect", output: "DESIGN: add func"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	res, err := r.runTDDSequence(s)
	if err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}
	if res == nil || res.Output != "IMPLEMENTED" {
		t.Fatalf("expected implementer result, got %+v", res)
	}

	wantOrder := []string{"architect", "test-author", "implementer"}
	if !equalStrings(*order, wantOrder) {
		t.Errorf("role order = %v, want %v", *order, wantOrder)
	}
	if s.tddArchitectDesign != "DESIGN: add func" {
		t.Errorf("architect design = %q, want captured advisory output", s.tddArchitectDesign)
	}
	if !equalStrings(s.tddTestNames, []string{"TestAdd"}) {
		t.Errorf("parsed test names = %v, want [TestAdd]", s.tddTestNames)
	}
	// Both gate invocations must scope to the authored test name on this Go repo.
	for _, cmd := range *gateCmds {
		if !strings.Contains(cmd, "TestAdd") || !strings.Contains(cmd, "-run") {
			t.Errorf("gate command %q not scoped to TestAdd", cmd)
		}
	}
	if len(*gateCmds) != 2 {
		t.Errorf("expected 2 gate invocations (RED, GREEN), got %d", len(*gateCmds))
	}
}

// TestRunTDDSequenceRedNotRedAborts verifies that when the authored tests PASS
// before implementation (RED gate not red) and stay passing after the one
// allowed test-author retry, the run ABORTS with reasonTDDRedGateNotRed and never
// reaches the implementer.
func TestRunTDDSequenceRedNotRedAborts(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	// RED gate returns pass() on every check => tests never fail => not red.
	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{pass()})

	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add passing test"),
	}
	implCalled := false
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		action: func() { implCalled = true },
	}

	res, err := r.runTDDSequence(s)
	if err == nil || !strings.Contains(err.Error(), reasonTDDRedGateNotRed) {
		t.Fatalf("expected %s abort, got res=%+v err=%v", reasonTDDRedGateNotRed, res, err)
	}
	if implCalled {
		t.Error("implementer must NOT run when the RED gate never goes red")
	}
	// architect + initial test-author + one test-author retry => no implementer.
	wantOrder := []string{"architect", "test-author", "test-author"}
	if !equalStrings(*order, wantOrder) {
		t.Errorf("role order = %v, want %v", *order, wantOrder)
	}
}
