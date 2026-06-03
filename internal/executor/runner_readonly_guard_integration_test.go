package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// readOnlyRecordingBackend is a Backend whose Execute simulates a misbehaving
// read-only role (PLAN / ARCHITECT) running on a non-claude executor that ignores
// AllowedTools: it WRITES a file into the worktree (the live architect-leak we
// observed, where the TDD architect left calc.go behind) and, when commit is set,
// also COMMITs it. Everything it touches is a trace the guard must discard.
type readOnlyRecordingBackend struct {
	dir      string
	fileName string
	content  string
	commit   bool
	executed bool
}

func (b *readOnlyRecordingBackend) Name() string      { return "read-only-recording" }
func (b *readOnlyRecordingBackend) IsAvailable() bool { return true }

func (b *readOnlyRecordingBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.executed = true
	path := filepath.Join(b.dir, b.fileName)
	if err := os.WriteFile(path, []byte(b.content), 0o644); err != nil {
		return nil, err
	}
	if b.commit {
		for _, args := range [][]string{{"add", "."}, {"commit", "-m", "read-only role stray commit"}} {
			cmd := exec.CommandContext(ctx, "git", append([]string{"-C", b.dir}, args...)...)
			if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
				return nil, &execError{msg: string(out), err: cmdErr}
			}
		}
	}
	return &BackendResult{Success: true, Output: "design only, no implementation"}, nil
}

type execError struct {
	msg string
	err error
}

func (e *execError) Error() string { return e.msg + ": " + e.err.Error() }
func (e *execError) Unwrap() error { return e.err }

// runReadOnlyRoleUnderGuard reproduces the controller's read-only guard call
// pattern exactly as executePipelinePlan / the TDD architect path do it: capture
// HEAD before the role, run the role's backend Execute, then enforceReadOnly.
func runReadOnlyRoleUnderGuard(t *testing.T, git *GitOperations, headBefore, role string, backend Backend, projectPath string) readOnlyGuardResult {
	t.Helper()
	ctx := context.Background()
	if _, err := backend.Execute(ctx, ExecuteOptions{
		Prompt:       "design a change; do not implement",
		ProjectPath:  projectPath,
		AllowedTools: DefaultAllowedToolsPlanning(),
	}); err != nil {
		t.Fatalf("recording backend Execute: %v", err)
	}
	return enforceReadOnly(ctx, git, headBefore, role, quietLogger())
}

// assertPristine asserts the worktree is back to headBefore with a clean status
// and that the role's stray file is gone — the live equivalent of the next role
// (test-author) seeing a clean tree.
func assertPristine(t *testing.T, git *GitOperations, dir, headBefore, strayFile string) {
	t.Helper()
	ctx := context.Background()

	nowHead, err := git.GetCurrentCommitSHA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCommitSHA: %v", err)
	}
	if nowHead != headBefore {
		t.Errorf("HEAD = %q after guard, want pristine headBefore %q", nowHead, headBefore)
	}
	if _, err := os.Stat(filepath.Join(dir, strayFile)); !os.IsNotExist(err) {
		t.Errorf("guard left stray file %q behind (err=%v); next role would see it", strayFile, err)
	}
	dirty, err := git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("worktree dirty after guard, want clean for the next role")
	}
}

// TestGuard_Integration_ReadOnlyRole_WritesUncommitted is the key integration
// test. A read-only role whose backend WRITES an uncommitted file into a real
// temp-git worktree runs under the guard; after the guard the worktree must be
// PRISTINE (file gone, HEAD == headBefore, clean status) and Violated == true.
// Before the fix the guard (SHA-only) no-ops on an unchanged HEAD and the file
// leaks — this fails. After the fix it passes.
func TestGuard_Integration_ReadOnlyRole_WritesUncommitted(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)

	backend := &readOnlyRecordingBackend{
		dir:      dir,
		fileName: "calc.go",
		content:  "package calc\n\nfunc Add(a, b int) int { return a + b }\n",
		commit:   false,
	}

	res := runReadOnlyRoleUnderGuard(t, git, head, pilotapi.RoleArchitect, backend, dir)

	if !backend.executed {
		t.Fatal("recording backend did not run")
	}
	if !res.Violated {
		t.Error("Violated = false, want true: an uncommitted write is a read-only trace")
	}
	if res.RevertedFrom != "" {
		t.Errorf("RevertedFrom = %q, want empty: the role did not commit", res.RevertedFrom)
	}
	assertPristine(t, git, dir, head, "calc.go")
}

// TestGuard_Integration_ReadOnlyRole_WritesAndCommits is the second case: the
// read-only role WRITES and COMMITs. The guard must still restore a pristine
// worktree (committed file gone, HEAD back to headBefore) and flag Violated,
// reporting the post-role HEAD it unwound.
func TestGuard_Integration_ReadOnlyRole_WritesAndCommits(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)

	backend := &readOnlyRecordingBackend{
		dir:      dir,
		fileName: "calc.go",
		content:  "package calc\n\nfunc Sub(a, b int) int { return a - b }\n",
		commit:   true,
	}

	ctx := context.Background()
	if _, err := backend.Execute(ctx, ExecuteOptions{ProjectPath: dir, AllowedTools: DefaultAllowedToolsPlanning()}); err != nil {
		t.Fatalf("recording backend Execute: %v", err)
	}
	afterRole, _ := git.GetCurrentCommitSHA(ctx)
	if afterRole == head {
		t.Fatal("precondition: committing role did not move HEAD")
	}

	res := enforceReadOnly(ctx, git, head, pilotapi.RolePlan, quietLogger())

	if !res.Violated {
		t.Error("Violated = false, want true after a read-only role committed")
	}
	if res.RevertedFrom != afterRole {
		t.Errorf("RevertedFrom = %q, want unwound HEAD %q", res.RevertedFrom, afterRole)
	}
	assertPristine(t, git, dir, head, "calc.go")
}

// TestGuard_Integration_WellBehavedReadOnlyRole verifies the no-op path end to
// end: a read-only role that produces only text (no worktree write) is left
// untouched — worktree unchanged and Violated == false.
func TestGuard_Integration_WellBehavedReadOnlyRole(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()

	// A well-behaved role: emit design text, write nothing.
	res := enforceReadOnly(ctx, git, head, pilotapi.RoleArchitect, quietLogger())

	if res.Violated {
		t.Error("Violated = true, want false for a read-only role that wrote nothing")
	}
	if res.RevertedFrom != "" {
		t.Errorf("RevertedFrom = %q, want empty when nothing was reverted", res.RevertedFrom)
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD moved on a no-op guard: %q != %q", nowHead, head)
	}
	if _, err := os.Stat(filepath.Join(dir, "seed.txt")); err != nil {
		t.Errorf("guard touched the pristine tree: seed.txt missing: %v", err)
	}
}
