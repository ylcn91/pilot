package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestExecuteLintPushPR_RevalidatesAfterLintAutoFix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub requires a POSIX shell")
	}

	const branch = "pilot/GH-7003"
	dir := setupLintGateRepo(t, branch)
	installFakeGolangciLint(t)

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/lintfix\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	runGit(t, dir, "add", "go.mod")
	runGit(t, dir, "commit", "-m", "chore: add go module")
	headBeforeLint := headSHA(t, dir)

	runner, _ := newCountingProgressRunner(t)
	runner.config = &BackendConfig{PrePushLint: boolPtr(true)}
	qualityChecks := 0
	runner.SetQualityCheckerFactory(func(_, projectPath string) QualityChecker {
		if projectPath != dir {
			t.Fatalf("quality checker projectPath = %q, want %q", projectPath, dir)
		}
		qualityChecks++
		return &mockQualityChecker{outcome: &QualityOutcome{Passed: false}}
	})

	task := &Task{
		ID:          "GH-7003",
		Title:       "feat: lint fix",
		ProjectPath: dir,
		Branch:      branch,
		BaseBranch:  "main",
		CreatePR:    true,
	}
	s := &executeState{
		task:               task,
		ctx:                context.Background(),
		log:                runner.log,
		git:                NewGitOperations(dir),
		executionPath:      dir,
		result:             &ExecutionResult{TaskID: task.ID, Success: true},
		state:              &progressState{phase: "Starting"},
		qualityGatesPassed: true,
	}

	res, err := runner.executeLintPushPR(s)
	if err != nil {
		t.Fatalf("executeLintPushPR: %v", err)
	}
	if res == nil || res.Success {
		t.Fatalf("expected post-lint quality failure, got %+v", res)
	}
	if !strings.Contains(res.Error, "pre-push lint fixes") {
		t.Fatalf("result error = %q, want post-lint failure", res.Error)
	}
	if qualityChecks != 1 {
		t.Fatalf("quality checks = %d, want 1", qualityChecks)
	}
	if headAfterLint := headSHA(t, dir); headAfterLint == headBeforeLint {
		t.Fatal("expected fake lint fix to amend the commit and move HEAD")
	}
}

func installFakeGolangciLint(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "golangci-lint")
	script := `#!/bin/sh
if [ "$1" = "run" ] && [ "$2" = "--fix" ]; then
  printf 'fixed\n' > lint-fixed.txt
  exit 0
fi
if [ "$1" = "run" ]; then
  if [ -f lint-fixed.txt ]; then
    exit 0
  fi
  echo 'lint issue'
  exit 1
fi
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake golangci-lint: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
