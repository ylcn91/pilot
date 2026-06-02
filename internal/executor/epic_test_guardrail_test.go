package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestCreateSubIssuesViaGitHub_GuardrailAllowsConfiguredRepo verifies the
// TASK-286 / GH-3027 guardrail: when a RepoAllowlist is wired AND the
// worktree's origin remote resolves to a configured repo, the gh CLI call
// proceeds normally.
func TestCreateSubIssuesViaGitHub_GuardrailAllowsConfiguredRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	worktree := t.TempDir()
	runGitForGuardrail(t, worktree, "init", "-q")
	runGitForGuardrail(t, worktree, "remote", "add", "origin", "https://github.com/ylcn91/pilot.git")

	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho https://github.com/ylcn91/pilot/issues/9999\n"), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+os.Getenv("PATH"))

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})

	plan := &EpicPlan{
		ParentTask: &Task{ID: "GH-42"},
		Subtasks: []PlannedSubtask{
			{Title: "feat(guardrail): allow happy path", Description: "ok", Order: 1},
		},
	}

	created, err := runner.CreateSubIssues(context.Background(), plan, worktree)
	if err != nil {
		t.Fatalf("guardrail unexpectedly blocked configured repo: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(created))
	}
}

// TestCreateSubIssuesViaGitHub_GuardrailBlocksUnmanagedRepo proves the
// incident-driving path is now closed: when the worktree's origin remote is
// NOT in the user's configured projects, no `gh issue create` call is fired
// and the error wraps ErrRepoNotInConfig.
//
// Without this guardrail, an external user pointing his Pilot at
// `ylcn91/pilot` created 6 dupes (#3021-#3026) on 2026-05-20.
func TestCreateSubIssuesViaGitHub_GuardrailBlocksUnmanagedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	worktree := t.TempDir()
	runGitForGuardrail(t, worktree, "init", "-q")
	runGitForGuardrail(t, worktree, "remote", "add", "origin", "https://github.com/tenlisboa/pilot-fork.git")

	// A `gh` shim that records its invocation. If the guardrail does its job,
	// this file must not exist after CreateSubIssues returns.
	callMarker := filepath.Join(t.TempDir(), "gh_was_called")
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	scriptBody := fmt.Sprintf("#!/bin/sh\ntouch %q\necho https://example/issues/0\n", callMarker)
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	t.Setenv(envBypassRepoAllowlist, "") // belt-and-braces: no stray bypass

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"alice/site"}}) // tenlisboa/pilot-fork intentionally missing

	plan := &EpicPlan{
		ParentTask: &Task{ID: "GH-42"},
		Subtasks: []PlannedSubtask{
			{Title: "feat(guardrail): should not run", Description: "blocked", Order: 1},
		},
	}

	_, err := runner.CreateSubIssues(context.Background(), plan, worktree)
	if err == nil {
		t.Fatal("expected guardrail to block unmanaged repo, got nil error")
	}
	if !errors.Is(err, ErrRepoNotInConfig) {
		t.Fatalf("error %v should wrap ErrRepoNotInConfig", err)
	}
	if _, statErr := os.Stat(callMarker); statErr == nil {
		t.Errorf("guardrail did not fire before `gh issue create`: marker %s exists", callMarker)
	}
}

// TestCreateSubIssuesViaGitHub_GuardrailBypassEnvVar documents that the
// PILOT_ALLOW_UNMANAGED_REPO=1 env var lets the call proceed even when the
// repo is not in the allowlist. The bypass logs a WARN inside
// ValidateTargetRepo; here we verify behavior (no error + gh fires).
func TestCreateSubIssuesViaGitHub_GuardrailBypassEnvVar(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	worktree := t.TempDir()
	runGitForGuardrail(t, worktree, "init", "-q")
	runGitForGuardrail(t, worktree, "remote", "add", "origin", "https://github.com/tenlisboa/pilot-fork.git")

	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho https://example/issues/1\n"), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	t.Setenv(envBypassRepoAllowlist, "1")

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"alice/site"}})

	plan := &EpicPlan{
		ParentTask: &Task{ID: "GH-42"},
		Subtasks: []PlannedSubtask{
			{Title: "feat(guardrail): bypass should proceed", Description: "ok via env", Order: 1},
		},
	}

	created, err := runner.CreateSubIssues(context.Background(), plan, worktree)
	if err != nil {
		t.Fatalf("PILOT_ALLOW_UNMANAGED_REPO=1 should let the call proceed: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 issue created via bypass, got %d", len(created))
	}
}

// TestCreateSubIssues_PollerSkipCalledForGitHubIssues verifies GH-3240: after
// createSubIssuesViaGitHub creates each sub-issue, it must call the
// SubIssuePollerSkipFn callback with the issue number so the poller marks it
// as processed and does not re-dispatch it on the next poll cycle.
func TestCreateSubIssues_PollerSkipCalledForGitHubIssues(t *testing.T) {
	// Fake "gh" binary that returns successive issue URLs.
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	scriptContent := "#!/bin/sh\n" +
		// Each call prints the next issue number (101, 102, …) by counting invocations
		// via a temp counter file, then emits a valid GitHub issue URL.
		`COUNT_FILE="` + filepath.Join(t.TempDir(), "count") + `"
if [ -f "$COUNT_FILE" ]; then
  N=$(cat "$COUNT_FILE")
else
  N=100
fi
N=$((N+1))
echo $N > "$COUNT_FILE"
echo "https://github.com/owner/repo/issues/$N"
`
	if err := os.WriteFile(script, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+origPATH)

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")

	var mu sync.Mutex
	var skipped []int
	runner.SetSubIssuePollerSkip(func(n int) {
		mu.Lock()
		skipped = append(skipped, n)
		mu.Unlock()
	})

	plan := &EpicPlan{
		ParentTask: &Task{ID: "GH-99"},
		Subtasks: []PlannedSubtask{
			{Title: "feat(sub): first subtask", Description: "First", Order: 1},
			{Title: "feat(sub): second subtask", Description: "Second", Order: 2},
		},
	}

	created, err := runner.CreateSubIssues(context.Background(), plan, worktree)
	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 created issues, got %d", len(created))
	}

	mu.Lock()
	got := append([]int(nil), skipped...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("SubIssuePollerSkipFn called %d times, want 2; skipped=%v", len(got), got)
	}
	for i, issue := range created {
		if got[i] != issue.Number {
			t.Errorf("skipped[%d] = %d, want %d (issue.Number)", i, got[i], issue.Number)
		}
	}
}
