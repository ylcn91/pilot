package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractPRURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple URL",
			input: "https://github.com/owner/repo/pull/123",
			want:  "https://github.com/owner/repo/pull/123",
		},
		{
			name:  "URL with already exists message",
			input: "a]ready exists:\nhttps://github.com/owner/repo/pull/456\n",
			want:  "https://github.com/owner/repo/pull/456",
		},
		{
			name:  "gh CLI already exists output",
			input: "a pull request for branch `feature` into `main` already exists:\nhttps://github.com/ylcn91/pilot/pull/285",
			want:  "https://github.com/ylcn91/pilot/pull/285",
		},
		{
			name:  "URL with trailing text",
			input: "https://github.com/owner/repo/pull/789 (created)",
			want:  "https://github.com/owner/repo/pull/789",
		},
		{
			name:  "no URL",
			input: "failed to create pull request",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "github URL but not PR",
			input: "https://github.com/owner/repo/issues/123",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRURL(tt.input)
			if got != tt.want {
				t.Errorf("extractPRURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestRemoteBranchExists_NoRemote validates that RemoteBranchExists returns false
// when there is no remote configured.
// GH-1389: This method is used to detect if push actually succeeded despite worktree errors.
func TestRemoteBranchExists_NoRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "pilot-git-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()

	// Initialize git repo with initial commit (no remote)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "config", "user.name", "Test User").Run()
	_ = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("initial"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", tmpDir, "commit", "-m", "initial").Run()

	git := NewGitOperations(tmpDir)

	// RemoteBranchExists should return false when no remote is configured
	exists := git.RemoteBranchExists(ctx, "main")
	if exists {
		t.Error("RemoteBranchExists should return false when no remote configured")
	}

	// Should also return false for any branch name
	exists = git.RemoteBranchExists(ctx, "nonexistent-branch")
	if exists {
		t.Error("RemoteBranchExists should return false for nonexistent branch")
	}
}

// TestRemoteBranchExists_WithRemote validates that RemoteBranchExists correctly
// detects branches on a remote.
// GH-1389: This verifies the core fix for detecting successful pushes.
func TestRemoteBranchExists_WithRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a "remote" repo (bare)
	remoteDir, err := os.MkdirTemp("", "pilot-git-remote-*")
	if err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(remoteDir) }()

	// Create local repo
	localDir, err := os.MkdirTemp("", "pilot-git-local-*")
	if err != nil {
		t.Fatalf("failed to create local dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(localDir) }()

	ctx := context.Background()

	// Initialize bare remote repo
	_ = exec.CommandContext(ctx, "git", "-C", remoteDir, "init", "--bare").Run()

	// Initialize local repo
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "init").Run()
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "config", "user.email", "test@test.com").Run()
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "config", "user.name", "Test User").Run()

	// Add remote
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "remote", "add", "origin", remoteDir).Run()

	// Create initial commit
	_ = os.WriteFile(filepath.Join(localDir, "test.txt"), []byte("initial"), 0644)
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "add", ".").Run()
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "commit", "-m", "initial").Run()

	git := NewGitOperations(localDir)

	// Get current branch name
	currentBranch, _ := git.GetCurrentBranch(ctx)

	// Branch doesn't exist on remote yet (not pushed)
	exists := git.RemoteBranchExists(ctx, currentBranch)
	if exists {
		t.Error("RemoteBranchExists should return false before push")
	}

	// Push the branch
	_ = exec.CommandContext(ctx, "git", "-C", localDir, "push", "-u", "origin", currentBranch).Run()

	// Now branch should exist on remote
	exists = git.RemoteBranchExists(ctx, currentBranch)
	if !exists {
		t.Error("RemoteBranchExists should return true after push")
	}

	// Nonexistent branch should still return false
	exists = git.RemoteBranchExists(ctx, "nonexistent-branch-12345")
	if exists {
		t.Error("RemoteBranchExists should return false for nonexistent branch")
	}
}
