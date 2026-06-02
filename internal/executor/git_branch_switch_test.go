package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSwitchToDefaultBranchAndPull(t *testing.T) {
	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pilot-git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()

	// Initialize git repo
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()

	// Create initial commit on main
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("initial"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "initial").Run()

	git := NewGitOperations(tmpDir)

	// Get default branch name (main or master)
	defaultBranch, _ := git.GetCurrentBranch(ctx)

	// Create and switch to a feature branch
	_ = git.CreateBranch(ctx, "feature-branch")
	currentBranch, _ := git.GetCurrentBranch(ctx)
	if currentBranch != "feature-branch" {
		t.Fatalf("expected to be on feature-branch, got %s", currentBranch)
	}

	// Make a commit on feature branch
	_ = os.WriteFile(testFile, []byte("feature change"), 0644)
	_, _ = git.Commit(ctx, "feature commit")

	// Now SwitchToDefaultBranchAndPull should switch us back to main/master
	// Note: Pull will fail since there's no remote, but the function handles this gracefully
	branch, err := git.SwitchToDefaultBranchAndPull(ctx)
	if err != nil {
		// The switch should succeed even if pull fails (no remote)
		t.Logf("SwitchToDefaultBranchAndPull returned error (expected, no remote): %v", err)
	}

	if branch != defaultBranch {
		t.Errorf("returned branch = %q, want %q", branch, defaultBranch)
	}

	// Verify we're now on the default branch
	currentBranch, _ = git.GetCurrentBranch(ctx)
	if currentBranch != defaultBranch {
		t.Errorf("current branch = %q, want %q", currentBranch, defaultBranch)
	}
}

// TestSwitchToBranchAndPull verifies that SwitchToBranchAndPull honors an
// explicit branch override (GH-2290: project.default_branch / branch_from).
func TestSwitchToBranchAndPull(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmpDir, err := os.MkdirTemp("", "pilot-git-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "t@t").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "T").Run()

	testFile := filepath.Join(tmpDir, "a.txt")
	_ = os.WriteFile(testFile, []byte("x"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "init").Run()

	git := NewGitOperations(tmpDir)

	// Create a `dev` branch and a feature branch off of it.
	_ = git.CreateBranch(ctx, "dev")
	_ = os.WriteFile(testFile, []byte("dev"), 0644)
	_, _ = git.Commit(ctx, "dev commit")
	_ = git.CreateBranch(ctx, "feature")

	// Explicit override must switch to dev, not the git default.
	branch, err := git.SwitchToBranchAndPull(ctx, "dev")
	if err != nil {
		t.Logf("pull failed (expected, no remote): %v", err)
	}
	if branch != "dev" {
		t.Errorf("branch = %q, want dev", branch)
	}
	current, _ := git.GetCurrentBranch(ctx)
	if current != "dev" {
		t.Errorf("current = %q, want dev", current)
	}

	// Empty override should fall back to SwitchToDefaultBranchAndPull.
	if _, err := git.SwitchToBranchAndPull(ctx, ""); err != nil {
		t.Logf("fallback returned err (ok): %v", err)
	}
}

func TestSwitchToDefaultBranchAndPull_NewBranchFromMain(t *testing.T) {
	// This test verifies the fix for GH-279: new branches should fork from main, not previous branch
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pilot-git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()

	// Initialize git repo
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()

	// Create initial commit on main
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("main content"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "initial main commit").Run()

	git := NewGitOperations(tmpDir)
	defaultBranch, _ := git.GetCurrentBranch(ctx)

	// Get the main branch commit SHA
	mainSHA, _ := git.GetCurrentCommitSHA(ctx)

	// Create first feature branch and add commits (simulating pilot/GH-18)
	_ = git.CreateBranch(ctx, "pilot/GH-18")
	_ = os.WriteFile(filepath.Join(tmpDir, "feature1.txt"), []byte("feature 1"), 0644)
	_, _ = git.Commit(ctx, "feat: GH-18 changes")
	gh18SHA, _ := git.GetCurrentCommitSHA(ctx)

	// WITHOUT the fix: creating a new branch from here would fork from GH-18
	// WITH the fix: we switch to main first, so new branch forks from main

	// Switch to main first (this is what the fix does)
	_, _ = git.SwitchToDefaultBranchAndPull(ctx)

	// Create second feature branch (simulating pilot/GH-20)
	_ = git.CreateBranch(ctx, "pilot/GH-20")
	gh20ParentSHA, _ := git.GetCurrentCommitSHA(ctx)

	// The parent of GH-20 should be main, NOT GH-18
	if gh20ParentSHA != mainSHA {
		t.Errorf("GH-20 forked from wrong commit: got %s (GH-18=%s), want %s (main)", gh20ParentSHA, gh18SHA, mainSHA)
	}

	// Verify we're on the new branch
	currentBranch, _ := git.GetCurrentBranch(ctx)
	if currentBranch != "pilot/GH-20" {
		t.Errorf("expected to be on pilot/GH-20, got %s", currentBranch)
	}

	// Double-check: the main branch SHA should be our starting point
	_ = git.SwitchBranch(ctx, defaultBranch)
	currentMainSHA, _ := git.GetCurrentCommitSHA(ctx)
	if currentMainSHA != mainSHA {
		t.Errorf("main branch SHA changed unexpectedly: was %s, now %s", mainSHA, currentMainSHA)
	}
}

func TestCountNewCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "pilot-git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()

	// Initialize git repo with initial commit
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	_ = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("initial"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "initial").Run()

	git := NewGitOperations(tmpDir)
	defaultBranch, _ := git.GetCurrentBranch(ctx)

	// Create feature branch
	_ = git.CreateBranch(ctx, "pilot/GH-99")

	t.Run("zero commits on new branch", func(t *testing.T) {
		count, err := git.CountNewCommits(ctx, defaultBranch)
		if err != nil {
			t.Fatalf("CountNewCommits failed: %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
	})

	t.Run("one commit on branch", func(t *testing.T) {
		_ = os.WriteFile(filepath.Join(tmpDir, "feature.txt"), []byte("feature"), 0644)
		_, _ = git.Commit(ctx, "feat: add feature")

		count, err := git.CountNewCommits(ctx, defaultBranch)
		if err != nil {
			t.Fatalf("CountNewCommits failed: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})

	t.Run("multiple commits on branch", func(t *testing.T) {
		_ = os.WriteFile(filepath.Join(tmpDir, "feature2.txt"), []byte("feature2"), 0644)
		_, _ = git.Commit(ctx, "feat: add feature2")
		_ = os.WriteFile(filepath.Join(tmpDir, "feature3.txt"), []byte("feature3"), 0644)
		_, _ = git.Commit(ctx, "feat: add feature3")

		count, err := git.CountNewCommits(ctx, defaultBranch)
		if err != nil {
			t.Fatalf("CountNewCommits failed: %v", err)
		}
		if count != 3 {
			t.Errorf("count = %d, want 3", count)
		}
	})
}

// TestSwitchToDefaultBranchAndPull_FailsOnNonGitDir validates that
// SwitchToDefaultBranchAndPull returns an error for non-git directories.
// GH-836: This error MUST cause execution to abort (hard fail) rather than
// continuing on the wrong branch which corrupts PRs.
func TestSwitchToDefaultBranchAndPull_FailsOnNonGitDir(t *testing.T) {
	// Create temp directory without git init
	tmpDir, err := os.MkdirTemp("", "pilot-git-test-nogit-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()
	git := NewGitOperations(tmpDir)

	// SwitchToDefaultBranchAndPull should fail on non-git directory
	_, err = git.SwitchToDefaultBranchAndPull(ctx)
	if err == nil {
		t.Error("SwitchToDefaultBranchAndPull should fail on non-git directory")
	}
}

// TestSwitchBranch_FailsOnNonExistentBranch validates that
// SwitchBranch returns an error for non-existent branches.
// GH-836: This error MUST cause execution to abort when branch doesn't exist.
func TestSwitchBranch_FailsOnNonExistentBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "pilot-git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()

	// Initialize git repo with initial commit
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	_ = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("initial"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "initial").Run()

	git := NewGitOperations(tmpDir)

	// SwitchBranch should fail on non-existent branch
	err = git.SwitchBranch(ctx, "nonexistent-branch-xyz123")
	if err == nil {
		t.Error("SwitchBranch should fail on non-existent branch")
	}
}
