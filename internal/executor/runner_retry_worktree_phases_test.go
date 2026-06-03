//go:build integration

package executor

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// phaseRetryRecordingBackend records the ProjectPath of every Execute call so a
// test can assert which working tree the quality-gate and intent-correction
// retries ran in. It reports success but produces no commit, which is all the
// two retry phases need.
type phaseRetryRecordingBackend struct {
	mu           sync.Mutex
	projectPaths []string
}

func (b *phaseRetryRecordingBackend) Name() string      { return "phase-retry-recording" }
func (b *phaseRetryRecordingBackend) IsAvailable() bool { return true }

func (b *phaseRetryRecordingBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.mu.Lock()
	b.projectPaths = append(b.projectPaths, opts.ProjectPath)
	b.mu.Unlock()
	return &BackendResult{Success: true, Output: "retry executed"}, nil
}

func (b *phaseRetryRecordingBackend) lastPath() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.projectPaths) == 0 {
		return "", false
	}
	return b.projectPaths[len(b.projectPaths)-1], true
}

// failingThenPassingChecker fails (with ShouldRetry) on its first Check call to
// force the quality-gate retry, then passes, so the retry loop runs exactly one
// backend retry and exits cleanly.
type failingThenPassingChecker struct {
	mu    sync.Mutex
	calls int
}

func (c *failingThenPassingChecker) Check(ctx context.Context) (*QualityOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return &QualityOutcome{
			Passed:        false,
			ShouldRetry:   true,
			RetryFeedback: "build failed: fix it",
			Attempt:       1,
		}, nil
	}
	return &QualityOutcome{Passed: true, Attempt: c.calls}, nil
}

// newPhaseRetryState builds an executeState whose executionPath (the active
// worktree) is deliberately DISTINCT from task.ProjectPath (the original repo
// path). git points at the worktree, which carries a committed diff vs main so
// the intent judge has something to veto.
func newPhaseRetryState(t *testing.T, worktree, origRepo string) *executeState {
	t.Helper()
	task := &Task{
		ID:          "GH-B2",
		Title:       "B2 retry worktree path",
		Description: "ensure quality/intent retries run in the worktree",
		ProjectPath: origRepo, // ORIGINAL repo path — retries must NOT use this
		Branch:      "pilot/GH-B2",
		BaseBranch:  "main",
		CreatePR:    true,
	}
	return &executeState{
		start:          time.Now(),
		task:           task,
		ctx:            context.Background(),
		executionPath:  worktree, // the active worktree — retries MUST use this
		log:            slog.Default(),
		git:            NewGitOperations(worktree),
		agentPath:      filepath.Join(worktree, ".agent"),
		state:          &progressState{},
		result:         &ExecutionResult{Success: true},
		selectedModel:  "test-model",
		selectedEffort: "medium",
	}
}

// setupWorktreeWithDiff creates a git repo (standing in for the worktree) that
// has a commit on top of main, so GetDiff(ctx, "main") returns a non-empty diff.
// Returns the worktree path.
func setupWorktreeWithDiff(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pilot-worktree-b2-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Repo\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	// Branch + change so base...HEAD diff is non-empty.
	run("checkout", "-b", "pilot/GH-B2")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "add feature")
	return dir
}

// TestRunner_QualityRetry_RunsInWorktree is the B2 regression guard for the
// quality-gate feedback retry. Before the fix the retry passed
// ProjectPath: task.ProjectPath (the ORIGINAL repo), leaking the retry into the
// user's real checkout instead of the isolated worktree. With the fix it runs in
// s.executionPath.
func TestRunner_QualityRetry_RunsInWorktree(t *testing.T) {
	worktree := setupWorktreeWithDiff(t)
	defer func() { _ = os.RemoveAll(worktree) }()
	origRepo := t.TempDir() // a DIFFERENT path than the worktree

	backend := &phaseRetryRecordingBackend{}
	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.qualityCheckerFactory = func(taskID, projectPath string) QualityChecker {
		return &failingThenPassingChecker{}
	}

	s := newPhaseRetryState(t, worktree, origRepo)

	if _, err := runner.executeQualityGates(s); err != nil {
		t.Fatalf("executeQualityGates returned error: %v", err)
	}

	got, ok := backend.lastPath()
	if !ok {
		t.Fatalf("quality-gate retry never invoked the backend; expected one retry call")
	}
	if got == s.task.ProjectPath {
		t.Fatalf("quality retry ran in task.ProjectPath %q; it must run in the worktree %q", s.task.ProjectPath, s.executionPath)
	}
	if got != s.executionPath {
		t.Fatalf("quality retry ProjectPath = %q, want worktree %q", got, s.executionPath)
	}
}

// TestRunner_IntentRetry_RunsInWorktree is the B2 regression guard for the
// intent-correction retry. Before the fix the retry passed
// ProjectPath: task.ProjectPath (the ORIGINAL repo). With the fix it runs in
// s.executionPath.
func TestRunner_IntentRetry_RunsInWorktree(t *testing.T) {
	worktree := setupWorktreeWithDiff(t)
	defer func() { _ = os.RemoveAll(worktree) }()
	origRepo := t.TempDir() // a DIFFERENT path than the worktree

	backend := &phaseRetryRecordingBackend{}
	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	// Disable the parallel self-review so the ONLY backend call is the intent
	// retry. Self-review legitimately reviews task.ProjectPath and would
	// otherwise pollute lastPath(); it is out of scope for B2.
	runner.config = &BackendConfig{SkipSelfReview: true}
	// Intent judge that always vetoes, driving the intent-correction retry.
	// The re-judge after the retry also vetoes; we only care which path the
	// retry ran in.
	runner.intentJudge = newIntentJudgeWithRunner(
		func(ctx context.Context, args ...string) ([]byte, error) {
			return []byte("VERDICT:FAIL\nintent mismatch\nCONFIDENCE:0.9"), nil
		},
	)
	// qualityCheckerFactory stays nil → runSelfReview gates on CreatePR only and
	// the self-review subprocess is harmless here; the intent retry is the focus.
	runner.qualityCheckerFactory = nil

	s := newPhaseRetryState(t, worktree, origRepo)
	s.qualityGatesPassed = true

	if _, err := runner.executeSelfReviewIntent(s); err != nil {
		t.Fatalf("executeSelfReviewIntent returned error: %v", err)
	}

	got, ok := backend.lastPath()
	if !ok {
		t.Fatalf("intent-correction retry never invoked the backend; expected one retry call")
	}
	if got == s.task.ProjectPath {
		t.Fatalf("intent retry ran in task.ProjectPath %q; it must run in the worktree %q", s.task.ProjectPath, s.executionPath)
	}
	if got != s.executionPath {
		t.Fatalf("intent retry ProjectPath = %q, want worktree %q", got, s.executionPath)
	}
}
