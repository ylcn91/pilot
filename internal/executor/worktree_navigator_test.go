package executor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyNavigatorToWorktree tests copying .agent/ directory to worktree
func TestCopyNavigatorToWorktree(t *testing.T) {
	// Create source repo with .agent/ directory
	sourceRepo := t.TempDir()
	agentDir := filepath.Join(sourceRepo, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create .agent dir: %v", err)
	}

	// Create some Navigator files
	devReadme := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	if err := os.WriteFile(devReadme, []byte("# Navigator\n\nProject docs"), 0644); err != nil {
		t.Fatalf("failed to create DEVELOPMENT-README.md: %v", err)
	}

	// Create nested structure (.context-markers is commonly gitignored)
	markersDir := filepath.Join(agentDir, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		t.Fatalf("failed to create .context-markers: %v", err)
	}
	markerFile := filepath.Join(markersDir, "test-marker.md")
	if err := os.WriteFile(markerFile, []byte("# Marker"), 0644); err != nil {
		t.Fatalf("failed to create marker file: %v", err)
	}

	// Create worktree destination (empty)
	worktreePath := t.TempDir()

	// Copy Navigator to worktree
	if err := CopyNavigatorToWorktree(sourceRepo, worktreePath); err != nil {
		t.Fatalf("CopyNavigatorToWorktree failed: %v", err)
	}

	// Verify files were copied
	destReadme := filepath.Join(worktreePath, ".agent", "DEVELOPMENT-README.md")
	if _, err := os.Stat(destReadme); err != nil {
		t.Errorf("DEVELOPMENT-README.md not copied: %v", err)
	}

	// Verify nested content was copied
	destMarker := filepath.Join(worktreePath, ".agent", ".context-markers", "test-marker.md")
	if _, err := os.Stat(destMarker); err != nil {
		t.Errorf(".context-markers/test-marker.md not copied: %v", err)
	}

	// Verify content is correct
	content, err := os.ReadFile(destReadme)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(content) != "# Navigator\n\nProject docs" {
		t.Errorf("content mismatch: got %q", string(content))
	}
}

// TestCopyNavigatorToWorktree_NoAgent tests when source has no .agent/
func TestCopyNavigatorToWorktree_NoAgent(t *testing.T) {
	sourceRepo := t.TempDir()
	worktreePath := t.TempDir()

	// Should succeed (no-op) when .agent/ doesn't exist
	if err := CopyNavigatorToWorktree(sourceRepo, worktreePath); err != nil {
		t.Errorf("expected no error for missing .agent, got: %v", err)
	}

	// Verify .agent/ wasn't created in worktree
	destAgent := filepath.Join(worktreePath, ".agent")
	if _, err := os.Stat(destAgent); !os.IsNotExist(err) {
		t.Error("expected .agent/ to not exist in worktree")
	}
}

// TestCopyNavigatorToWorktree_Merge tests merging when worktree already has .agent/
func TestCopyNavigatorToWorktree_Merge(t *testing.T) {
	sourceRepo := t.TempDir()
	worktreePath := t.TempDir()

	// Create .agent/ in source with untracked content
	sourceAgent := filepath.Join(sourceRepo, ".agent")
	if err := os.MkdirAll(sourceAgent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceAgent, "untracked.md"), []byte("untracked"), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate worktree already having .agent/ from git (tracked content)
	destAgent := filepath.Join(worktreePath, ".agent")
	if err := os.MkdirAll(destAgent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destAgent, "tracked.md"), []byte("tracked"), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy should merge (add untracked.md, keep tracked.md)
	if err := CopyNavigatorToWorktree(sourceRepo, worktreePath); err != nil {
		t.Fatalf("CopyNavigatorToWorktree failed: %v", err)
	}

	// Verify both files exist
	if _, err := os.Stat(filepath.Join(destAgent, "tracked.md")); err != nil {
		t.Error("tracked.md should still exist")
	}
	if _, err := os.Stat(filepath.Join(destAgent, "untracked.md")); err != nil {
		t.Error("untracked.md should have been copied")
	}
}

// TestEnsureNavigatorInWorktree tests the high-level function
func TestEnsureNavigatorInWorktree(t *testing.T) {
	sourceRepo := t.TempDir()
	worktreePath := t.TempDir()

	// Create .agent/ in source
	sourceAgent := filepath.Join(sourceRepo, ".agent")
	if err := os.MkdirAll(sourceAgent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceAgent, "DEVELOPMENT-README.md"), []byte("# Nav"), 0644); err != nil {
		t.Fatal(err)
	}

	// Ensure Navigator in worktree
	if err := EnsureNavigatorInWorktree(sourceRepo, worktreePath); err != nil {
		t.Fatalf("EnsureNavigatorInWorktree failed: %v", err)
	}

	// Verify .agent/ exists in worktree
	destReadme := filepath.Join(worktreePath, ".agent", "DEVELOPMENT-README.md")
	if _, err := os.Stat(destReadme); err != nil {
		t.Errorf("Navigator not copied to worktree: %v", err)
	}
}

// TestEnsureNavigatorInWorktree_NoSource tests when source has no Navigator
func TestEnsureNavigatorInWorktree_NoSource(t *testing.T) {
	sourceRepo := t.TempDir()
	worktreePath := t.TempDir()

	// Should succeed - will defer to maybeInitNavigator in runner
	if err := EnsureNavigatorInWorktree(sourceRepo, worktreePath); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
