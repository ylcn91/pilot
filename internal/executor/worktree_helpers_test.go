package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestRepo creates a temporary git repository for testing worktrees.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	// Create temp directory
	dir, err := os.MkdirTemp("", "worktree-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Initialize git repo with main branch
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()

	// Create initial commit (required for worktree)
	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("failed to create test file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("failed to create initial commit: %v", err)
	}

	return dir
}

// setupTestRepoWithRemote creates a local repo with a "remote" for push testing.
// Returns (localRepo, remoteRepo) paths.
func setupTestRepoWithRemote(t *testing.T) (string, string) {
	t.Helper()

	// Create "remote" bare repository
	remoteDir, err := os.MkdirTemp("", "worktree-remote-*")
	if err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}

	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(remoteDir)
		t.Fatalf("failed to init bare repo: %v", err)
	}

	// Create local repository
	localDir, err := os.MkdirTemp("", "worktree-local-*")
	if err != nil {
		_ = os.RemoveAll(remoteDir)
		t.Fatalf("failed to create local dir: %v", err)
	}

	cmd = exec.Command("git", "init", "-b", "main")
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(remoteDir)
		_ = os.RemoveAll(localDir)
		t.Fatalf("failed to init local repo: %v", err)
	}

	// Configure git user
	_ = exec.Command("git", "-C", localDir, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", localDir, "config", "user.name", "Test User").Run()

	// Create initial commit
	testFile := filepath.Join(localDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		_ = os.RemoveAll(remoteDir)
		_ = os.RemoveAll(localDir)
		t.Fatalf("failed to create test file: %v", err)
	}

	_ = exec.Command("git", "-C", localDir, "add", ".").Run()
	cmd = exec.Command("git", "-C", localDir, "commit", "-m", "Initial commit")
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(remoteDir)
		_ = os.RemoveAll(localDir)
		t.Fatalf("failed to commit: %v", err)
	}

	// Add remote
	cmd = exec.Command("git", "-C", localDir, "remote", "add", "origin", remoteDir)
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(remoteDir)
		_ = os.RemoveAll(localDir)
		t.Fatalf("failed to add remote: %v", err)
	}

	// Push initial commit to remote
	cmd = exec.Command("git", "-C", localDir, "push", "-u", "origin", "HEAD:main")
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(remoteDir)
		_ = os.RemoveAll(localDir)
		t.Fatalf("failed to push to remote: %v", err)
	}

	return localDir, remoteDir
}
