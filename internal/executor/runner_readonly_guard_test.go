package executor

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// readOnlyTestRepo builds a temp git repo with one initial commit on a feature
// branch and returns the dir, a GitOperations rooted there, and the initial SHA.
// HEAD is a real commit so the guard's before/after SHA comparison is exercised.
func readOnlyTestRepo(t *testing.T) (dir string, git *GitOperations, head string) {
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
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	run("checkout", "-b", "pilot/GH-ro")

	git = NewGitOperations(dir)
	head, err := git.GetCurrentCommitSHA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCommitSHA: %v", err)
	}
	return dir, git, head
}

// commitNamed writes a file and commits it in dir, returning nothing. Used to
// simulate a misbehaving read-only role that lands a commit.
func commitNamed(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", msg}} {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestEnforceReadOnly_RevertsStrayCommit verifies the core contract: a read-only
// role that created a commit is flagged as a violation and soft-reverted to the
// captured HEAD, so HEAD returns to headBefore while the committed file survives
// as an uncommitted change (git reset --soft).
func TestEnforceReadOnly_RevertsStrayCommit(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	commitNamed(t, dir, "stray.txt", "design notes\n", "design: stray commit")

	after, _ := git.GetCurrentCommitSHA(ctx)
	if after == head {
		t.Fatal("precondition: stray commit did not move HEAD")
	}

	res := enforceReadOnly(ctx, git, head, pilotapi.RolePlan, quietLogger())

	if !res.Violated {
		t.Fatal("Violated = false, want true after a read-only role committed")
	}
	if res.RevertedFrom != after {
		t.Errorf("RevertedFrom = %q, want post-role HEAD %q", res.RevertedFrom, after)
	}

	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD = %q after revert, want captured head %q", nowHead, head)
	}
	// --soft preserves the work: the file is still present and staged.
	if _, err := os.Stat(filepath.Join(dir, "stray.txt")); err != nil {
		t.Errorf("soft revert lost working changes: stray.txt missing: %v", err)
	}
	count, err := git.CountNewCommits(ctx, "main")
	if err == nil && count != 0 {
		t.Errorf("CountNewCommits after revert = %d, want 0", count)
	}
}

// TestEnforceReadOnly_WellBehavedNoop verifies a read-only role that did NOT
// commit (HEAD unchanged) passes untouched: no violation, no revert.
func TestEnforceReadOnly_WellBehavedNoop(t *testing.T) {
	_, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	res := enforceReadOnly(ctx, git, head, pilotapi.RoleArchitect, quietLogger())

	if res.Violated {
		t.Error("Violated = true, want false for a role that left HEAD unchanged")
	}
	if res.RevertedFrom != "" {
		t.Errorf("RevertedFrom = %q, want empty when nothing reverted", res.RevertedFrom)
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD moved on a no-op guard: %q != %q", nowHead, head)
	}
}

// TestEnforceReadOnly_PreservesUncommittedWork verifies the guard is a no-op when
// a well-behaved read-only role left only uncommitted working-tree changes (no
// commit). Such changes must NOT be reverted — only stray commits are.
func TestEnforceReadOnly_PreservesUncommittedWork(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatalf("write wip: %v", err)
	}

	res := enforceReadOnly(ctx, git, head, pilotapi.RolePlan, quietLogger())

	if res.Violated {
		t.Error("Violated = true, want false: uncommitted changes are not a commit violation")
	}
	if _, err := os.Stat(filepath.Join(dir, "wip.txt")); err != nil {
		t.Errorf("guard touched uncommitted work: wip.txt missing: %v", err)
	}
}

// TestEnforceReadOnly_RevertsMultipleStrayCommits verifies the worst case: a
// misbehaving role that lands several commits is fully unwound back to the
// captured HEAD in one soft reset.
func TestEnforceReadOnly_RevertsMultipleStrayCommits(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	commitNamed(t, dir, "a.txt", "a\n", "design: commit 1")
	commitNamed(t, dir, "b.txt", "b\n", "design: commit 2")
	commitNamed(t, dir, "c.txt", "c\n", "design: commit 3")

	res := enforceReadOnly(ctx, git, head, pilotapi.RoleArchitect, quietLogger())

	if !res.Violated {
		t.Fatal("Violated = false, want true after three stray commits")
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD = %q, want captured head %q after multi-commit revert", nowHead, head)
	}
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("soft revert lost %s: %v", f, err)
		}
	}
}

// TestEnforceReadOnly_NilGit verifies the guard is a safe no-op when no git layer
// is wired (it must never panic or abort the run).
func TestEnforceReadOnly_NilGit(t *testing.T) {
	res := enforceReadOnly(context.Background(), nil, "deadbeef", pilotapi.RolePlan, quietLogger())
	if res.Violated || res.RevertedFrom != "" {
		t.Errorf("nil-git guard = %+v, want zero result", res)
	}
}

// TestEnforceReadOnly_EmptyHeadBefore verifies that a missing captured HEAD (the
// pre-role capture failed) disables enforcement rather than mis-reverting.
func TestEnforceReadOnly_EmptyHeadBefore(t *testing.T) {
	_, git, _ := readOnlyTestRepo(t)
	res := enforceReadOnly(context.Background(), git, "", pilotapi.RolePlan, quietLogger())
	if res.Violated || res.RevertedFrom != "" {
		t.Errorf("empty-headBefore guard = %+v, want zero result", res)
	}
}

// TestEnforceReadOnly_NilLogger verifies the guard does not panic when log is nil
// on the violation path (warn/revert both reference the logger).
func TestEnforceReadOnly_NilLogger(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()
	commitNamed(t, dir, "stray.txt", "x\n", "design: stray")

	res := enforceReadOnly(ctx, git, head, pilotapi.RolePlan, nil)
	if !res.Violated {
		t.Error("Violated = false with nil logger, want true")
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Error("nil-logger guard did not revert")
	}
}

// TestSoftResetTo_EmptySHA verifies SoftResetTo rejects an empty target instead
// of running a dangerous bare `git reset --soft`.
func TestSoftResetTo_EmptySHA(t *testing.T) {
	_, git, _ := readOnlyTestRepo(t)
	if err := git.SoftResetTo(context.Background(), "  "); err == nil {
		t.Fatal("SoftResetTo(empty) = nil, want error")
	}
}

// TestSoftResetTo_PreservesIndexAndWorktree verifies the --soft semantics
// directly: after reset, HEAD is at the target but the post-target file remains
// in the working tree.
func TestSoftResetTo_PreservesIndexAndWorktree(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()
	commitNamed(t, dir, "later.txt", "later\n", "later commit")

	if err := git.SoftResetTo(ctx, head); err != nil {
		t.Fatalf("SoftResetTo: %v", err)
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD = %q, want %q", nowHead, head)
	}
	if _, err := os.Stat(filepath.Join(dir, "later.txt")); err != nil {
		t.Errorf("--soft dropped working file: %v", err)
	}
}
