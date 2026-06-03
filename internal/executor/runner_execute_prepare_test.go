package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestExecutePrepare_CleansHookScriptsOnWriteError verifies the hook-setup path
// does not leak a pilot-hooks-* temp directory when WriteEmbeddedScripts fails
// after MkdirTemp already created the directory. The write is forced to fail by
// running MkdirTemp under a write-stripping umask, so the created tempdir is
// read-only and the embedded script writes inside it fail with permission
// denied — exercising the post-error os.RemoveAll cleanup branch.
func TestExecutePrepare_CleansHookScriptsOnWriteError(t *testing.T) {
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)
	if got := os.TempDir(); got != tmpRoot {
		t.Skipf("os.TempDir() = %q, not honoring TMPDIR override %q", got, tmpRoot)
	}

	projectPath := t.TempDir()

	r := newTestRunner("claude")
	r.backend = &recordingPlanBackend{}
	r.config.UseWorktree = false
	r.config.Hooks = &HooksConfig{Enabled: true}

	s := &executeState{
		task: &Task{
			ID:          "GH-prep-1",
			Title:       "do work",
			ProjectPath: projectPath,
		},
		ctx:           context.Background(),
		executionPath: projectPath,
	}

	// Strip write bits so MkdirTemp creates the pilot-hooks-* dir as 0500: the
	// dir is created (MkdirTemp succeeds), but WriteEmbeddedScripts cannot write
	// the embedded scripts into it, triggering the write-error cleanup path.
	oldUmask := syscall.Umask(0o222)
	res, err := r.executePrepare(s)
	syscall.Umask(oldUmask)

	if err != nil {
		t.Fatalf("executePrepare returned error: %v", err)
	}
	if res != nil {
		t.Fatalf("executePrepare returned result %+v, want nil on success path", res)
	}
	if s.hookRestoreFunc != nil {
		t.Fatalf("hookRestoreFunc set despite hook script write failure")
	}

	entries, readErr := os.ReadDir(tmpRoot)
	if readErr != nil {
		t.Fatalf("failed to read temp root: %v", readErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pilot-hooks-") {
			// Restore perms so t.TempDir cleanup can remove it, then fail.
			_ = os.Chmod(filepath.Join(tmpRoot, e.Name()), 0o700)
			t.Fatalf("leaked hook script tempdir %q after write error", e.Name())
		}
	}
}
