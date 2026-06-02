package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temp git repo with a user config and initial commit,
// returning the repo path and a cleanup func.
func initTestRepo(t *testing.T) (string, func()) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test User")

	// Initial commit so HEAD exists.
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("root"), 0644)
	run("add", "README.md")
	run("commit", "-m", "init")
	return dir, func() {} // t.TempDir cleans up automatically
}

// TestIsExcluded covers the isExcluded helper with a table of known cases.
func TestIsExcluded(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".agent/tasks/TASK-99.md", true},
		{".claude/settings.json", true},
		{"node_modules/pkg/index.js", true},
		{"node_modules/foo", true},
		{"dist/bundle.js", true},
		{"build/out.o", true},
		{"coverage/lcov.info", true},
		{".cache/foo", true},
		{"package-lock.json", true},
		{"yarn.lock", true},
		{".DS_Store", true},
		{"Thumbs.db", true},
		// prefix must not substring-match
		{".agentless/foo.md", false},
		// regular source files must not be excluded
		{"internal/executor/git.go", false},
		{"cmd/pilot/main.go", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		got := isExcluded(tc.path)
		if got != tc.want {
			t.Errorf("isExcluded(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestCommitScopedStaging runs integration tests against a real temp git repo.
func TestCommitScopedStaging(t *testing.T) {
	dir, _ := initTestRepo(t)
	ctx := context.Background()
	git := NewGitOperations(dir)

	mkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, rel), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		mkdir(filepath.Dir(rel))
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	t.Run("pure code change commits cleanly", func(t *testing.T) {
		write("internal/foo.go", "package foo")
		sha, err := git.Commit(ctx, "feat: add foo")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		if !isValidSHA(sha) {
			t.Errorf("bad SHA: %q", sha)
		}
		// file should be committed (no longer untracked/modified)
		hasChanges, _ := git.HasUncommittedChanges(ctx)
		if hasChanges {
			t.Error("expected clean state after commit")
		}
	})

	t.Run("pure exclude change returns ErrNoStageableChanges", func(t *testing.T) {
		write(".agent/tasks/TASK-99.md", "# draft")
		_, err := git.Commit(ctx, "should not commit")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrNoStageableChanges) {
			t.Errorf("expected ErrNoStageableChanges, got: %v", err)
		}
		// excluded file must remain untracked (repo unchanged)
		cmd := exec.Command("git", "status", "--porcelain")
		cmd.Dir = dir
		out, _ := cmd.Output()
		if len(out) == 0 {
			t.Error("expected dirty state; excluded file should still be untracked")
		}
	})

	t.Run("mixed scope commits only code file", func(t *testing.T) {
		// .agent file already exists from previous sub-test; add a new code file.
		write("internal/bar.go", "package bar")
		sha, err := git.Commit(ctx, "feat: add bar")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		if !isValidSHA(sha) {
			t.Errorf("bad SHA: %q", sha)
		}
		// .agent/tasks/TASK-99.md should still be untracked
		cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard", ".agent/tasks/TASK-99.md")
		cmd.Dir = dir
		out, _ := cmd.Output()
		if len(out) == 0 {
			t.Error(".agent/tasks/TASK-99.md should remain untracked after mixed-scope commit")
		}
	})

	t.Run("lock file excluded", func(t *testing.T) {
		write("internal/baz.go", "package baz")
		write("package-lock.json", "{}")
		sha, err := git.Commit(ctx, "feat: add baz")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		if !isValidSHA(sha) {
			t.Errorf("bad SHA: %q", sha)
		}
		// lock file should remain untracked
		cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard", "package-lock.json")
		cmd.Dir = dir
		out, _ := cmd.Output()
		if len(out) == 0 {
			t.Error("package-lock.json should remain untracked after commit")
		}
	})

	t.Run("nested node_modules excluded", func(t *testing.T) {
		write("internal/qux.go", "package qux")
		write("node_modules/some-pkg/bin.js", "// bin")
		sha, err := git.Commit(ctx, "feat: add qux")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		if !isValidSHA(sha) {
			t.Errorf("bad SHA: %q", sha)
		}
		// node_modules entry should remain untracked
		cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard", "node_modules/some-pkg/bin.js")
		cmd.Dir = dir
		out, _ := cmd.Output()
		if len(out) == 0 {
			t.Error("node_modules/some-pkg/bin.js should remain untracked after commit")
		}
	})
}
