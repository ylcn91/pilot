package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestHardResetTo_EmptySHA verifies HardResetTo rejects an empty target instead
// of running a destructive bare `git reset --hard`.
func TestHardResetTo_EmptySHA(t *testing.T) {
	_, git, _ := readOnlyTestRepo(t)
	if err := git.HardResetTo(context.Background(), "  "); err == nil {
		t.Fatal("HardResetTo(empty) = nil, want error")
	}
}

// TestHardResetTo_DiscardsCommittedAndTracked verifies the --hard semantics the
// guard relies on: after reset, HEAD is at the target AND a post-target committed
// file is gone (unlike --soft, which preserves it).
func TestHardResetTo_DiscardsCommittedAndTracked(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()
	commitNamed(t, dir, "later.txt", "later\n", "later commit")

	if err := git.HardResetTo(ctx, head); err != nil {
		t.Fatalf("HardResetTo: %v", err)
	}
	nowHead, _ := git.GetCurrentCommitSHA(ctx)
	if nowHead != head {
		t.Errorf("HEAD = %q, want %q", nowHead, head)
	}
	if _, err := os.Stat(filepath.Join(dir, "later.txt")); !os.IsNotExist(err) {
		t.Errorf("--hard kept committed file later.txt (err=%v)", err)
	}
}

// TestHardResetTo_RestoresModifiedTrackedFile verifies a hard reset reverts a
// modified tracked file back to the committed content.
func TestHardResetTo_RestoresModifiedTrackedFile(t *testing.T) {
	dir, git, head := readOnlyTestRepo(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("MUTATED\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := git.HardResetTo(ctx, head); err != nil {
		t.Fatalf("HardResetTo: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "seed.txt"))
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if string(content) != "seed\n" {
		t.Errorf("seed.txt = %q, want restored %q", content, "seed\n")
	}
}

// TestCleanUntracked_RemovesUntrackedFilesAndDirs verifies CleanUntracked removes
// untracked files and directories that a hard reset leaves behind, while keeping
// tracked content intact.
func TestCleanUntracked_RemovesUntrackedFilesAndDirs(t *testing.T) {
	dir, git, _ := readOnlyTestRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	sub := filepath.Join(dir, "scratch")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "note.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatalf("write nested untracked: %v", err)
	}

	if err := git.CleanUntracked(ctx); err != nil {
		t.Fatalf("CleanUntracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "untracked.txt")); !os.IsNotExist(err) {
		t.Errorf("clean kept untracked.txt (err=%v)", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Errorf("clean kept untracked dir scratch/ (err=%v)", err)
	}
	// Tracked file survives.
	if _, err := os.Stat(filepath.Join(dir, "seed.txt")); err != nil {
		t.Errorf("clean removed tracked seed.txt: %v", err)
	}
}

// TestIsDirty verifies IsDirty reports true for both an untracked addition and a
// modified tracked file, and false on a clean tree.
func TestIsDirty(t *testing.T) {
	dir, git, _ := readOnlyTestRepo(t)
	ctx := context.Background()

	dirty, err := git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty(clean): %v", err)
	}
	if dirty {
		t.Error("IsDirty = true on a clean tree, want false")
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("z\n"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}
	dirty, err = git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty(untracked): %v", err)
	}
	if !dirty {
		t.Error("IsDirty = false with an untracked file, want true")
	}

	if err := git.CleanUntracked(ctx); err != nil {
		t.Fatalf("CleanUntracked: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("modify seed: %v", err)
	}
	dirty, err = git.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty(modified): %v", err)
	}
	if !dirty {
		t.Error("IsDirty = false with a modified tracked file, want true")
	}
}
