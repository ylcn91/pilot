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
// role that created a commit is flagged as a violation and the worktree is
// restored to a PRISTINE headBefore state — HEAD returns to headBefore AND the
// committed file is gone (git reset --hard, not --soft). The old soft-reset
// guard preserved the file; that leaked traces to the next role.
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
	// Pristine restore: the committed file must be gone (was tracked → hard reset).
	if _, err := os.Stat(filepath.Join(dir, "stray.txt")); !os.IsNotExist(err) {
		t.Errorf("pristine restore did not discard committed file: stray.txt still present (err=%v)", err)
	}
	dirty, err := git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("worktree dirty after pristine restore, want clean")
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

// TestEnforceReadOnly_DiscardsUncommittedWrite verifies the leak the old guard
// missed: a read-only role that WROTE a file WITHOUT committing leaves HEAD
// unchanged, so a SHA-only guard no-ops and the file leaks to the next role.
// The pristine-restore guard flags it as a violation and removes the untracked
// file even though HEAD never moved.
func TestEnforceReadOnly_DiscardsUncommittedWrite(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatalf("write wip: %v", err)
	}

	res := enforceReadOnly(ctx, git, head, pilotapi.RolePlan, quietLogger())

	if !res.Violated {
		t.Error("Violated = false, want true: an uncommitted write is a read-only trace")
	}
	// HEAD never moved, so there is nothing to report as RevertedFrom.
	if res.RevertedFrom != "" {
		t.Errorf("RevertedFrom = %q, want empty: no commit was made", res.RevertedFrom)
	}
	if _, err := os.Stat(filepath.Join(dir, "wip.txt")); !os.IsNotExist(err) {
		t.Errorf("guard left uncommitted write behind: wip.txt still present (err=%v)", err)
	}
	dirty, err := git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("worktree dirty after pristine restore, want clean")
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD = %q, want unchanged %q", nowHead, head)
	}
}

// TestEnforceReadOnly_DiscardsModifiedTrackedFile verifies the guard also reverts
// a modified TRACKED file (not just untracked additions) left uncommitted by a
// read-only role — `git status --porcelain` is non-empty and `git reset --hard`
// restores the original content.
func TestEnforceReadOnly_DiscardsModifiedTrackedFile(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	// seed.txt is tracked (committed in readOnlyTestRepo); overwrite it.
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("MUTATED\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	res := enforceReadOnly(ctx, git, head, pilotapi.RoleArchitect, quietLogger())

	if !res.Violated {
		t.Error("Violated = false, want true: a modified tracked file is a read-only trace")
	}
	content, err := os.ReadFile(filepath.Join(dir, "seed.txt"))
	if err != nil {
		t.Fatalf("read seed after restore: %v", err)
	}
	if string(content) != "seed\n" {
		t.Errorf("seed.txt = %q after restore, want original %q", content, "seed\n")
	}
	dirty, err := git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("worktree dirty after pristine restore, want clean")
	}
}

// TestEnforceReadOnly_RevertsMultipleStrayCommits verifies the worst case: a
// misbehaving role that lands several commits is fully unwound back to the
// captured HEAD in one hard reset, and every committed file is discarded so the
// worktree is pristine for the next role.
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
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("pristine restore did not discard %s (err=%v)", f, err)
		}
	}
	dirty, err := git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("worktree dirty after pristine restore, want clean")
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
