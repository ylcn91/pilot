package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// setupLintGateRepo builds a local git repo on `main` with one extra commit on
// `feature`. No remote is configured (push will fail), and no go.mod / golangci
// config exists so autoFixLint short-circuits to Clean without shelling out to a
// linter. The repo lets executeLintPushPR cross the no-commits guard and reach
// the pre-push lint gate deterministically.
func setupLintGateRepo(t *testing.T, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, err := os.MkdirTemp("", "pilot-lint-gate-*")
	if err != nil {
		t.Fatalf("setupLintGateRepo: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@pilot.local")
	run("config", "user.name", "Pilot Test")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatalf("setupLintGateRepo: WriteFile README: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")

	run("checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("change\n"), 0644); err != nil {
		t.Fatalf("setupLintGateRepo: WriteFile change: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "feat: implement change")
	return dir
}

// countingProgressRunner wires a Runner whose OnProgress callback records every
// "Linting" phase report. The lint gate emits exactly one "Linting" progress
// event immediately before each autoFixLint call, so the count of "Linting"
// events equals the number of times the lint gate fired on a given path.
func newCountingProgressRunner(t *testing.T) (*Runner, *lintEventCounter) {
	t.Helper()
	r := NewRunner()
	c := &lintEventCounter{}
	r.OnProgress(func(_, phase string, _ int, _ string) {
		if phase == "Linting" {
			c.inc()
		}
	})
	return r, c
}

type lintEventCounter struct {
	mu sync.Mutex
	n  int
}

func (c *lintEventCounter) inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *lintEventCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// TestExecuteLintPushPR_CreatePRPath_LintsOnce proves the PR (branch-push) path
// runs the pre-push lint gate exactly once. Previously executeLintPushPR ran the
// gate once unconditionally at the top AND again inside the CreatePR branch, so
// the PR path linted twice.
func TestExecuteLintPushPR_CreatePRPath_LintsOnce(t *testing.T) {
	const branch = "pilot/GH-7001"
	dir := setupLintGateRepo(t, branch)

	runner, counter := newCountingProgressRunner(t)
	runner.config = &BackendConfig{PrePushLint: boolPtr(true)}

	task := &Task{
		ID: "GH-7001",
		// Empty title makes normalizeTitle fail, returning before gh pr create —
		// after the lint gate has already run, which is what we are measuring.
		Title:       "",
		ProjectPath: dir,
		Branch:      branch,
		BaseBranch:  "main",
		CreatePR:    true,
	}

	git := NewGitOperations(dir)
	s := &executeState{
		task:          task,
		ctx:           context.Background(),
		log:           runner.log,
		git:           git,
		executionPath: dir,
		result:        &ExecutionResult{TaskID: task.ID, Success: true},
		state:         &progressState{phase: "Starting"},
	}

	_, _ = runner.executeLintPushPR(s)

	if got := counter.count(); got != 1 {
		t.Fatalf("CreatePR path ran the lint gate %d times, want exactly 1", got)
	}
}

// TestExecuteLintPushPR_DirectCommitPath_LintsOnce proves the direct-commit
// (push-to-main) path runs the pre-push lint gate exactly once.
func TestExecuteLintPushPR_DirectCommitPath_LintsOnce(t *testing.T) {
	dir := setupLintGateRepo(t, "pilot/GH-7002")

	runner, counter := newCountingProgressRunner(t)
	runner.config = &BackendConfig{PrePushLint: boolPtr(true)}

	task := &Task{
		ID:           "GH-7002",
		Title:        "feat: direct commit",
		ProjectPath:  dir,
		DirectCommit: true,
	}

	git := NewGitOperations(dir)
	s := &executeState{
		task:          task,
		ctx:           context.Background(),
		log:           runner.log,
		git:           git,
		executionPath: dir,
		result:        &ExecutionResult{TaskID: task.ID, Success: true},
		state:         &progressState{phase: "Starting"},
	}

	_, _ = runner.executeLintPushPR(s)

	if got := counter.count(); got != 1 {
		t.Fatalf("DirectCommit path ran the lint gate %d times, want exactly 1", got)
	}
}
