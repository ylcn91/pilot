package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeAndCommit writes a file in the repo and commits it on the current branch.
func writeAndCommit(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	ctx := context.Background()
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", msg}} {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestSnapshotAndVerifyUnchanged: snapshot a committed test file, then verify
// passes when nothing changes.
func TestSnapshotAndVerifyUnchanged(t *testing.T) {
	dir, base := tddGitRepo(t)
	writeAndCommit(t, dir, "feature_test.go", "package tddtest\n// red test\n", "test: add red")

	snap, err := snapshotTestFiles(context.Background(), dir, base)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Files) != 1 {
		t.Fatalf("snapshot files = %v, want 1 (_test.go only)", snap.Files)
	}
	if _, ok := snap.Files["feature_test.go"]; !ok {
		t.Fatalf("snapshot missing feature_test.go: %v", snap.Files)
	}
	if err := verifyTestFreeze(snap, dir); err != nil {
		t.Fatalf("verify unchanged should pass: %v", err)
	}
}

// TestVerifyDetectsModification: editing the frozen test file fails the guard.
func TestVerifyDetectsModification(t *testing.T) {
	dir, base := tddGitRepo(t)
	writeAndCommit(t, dir, "feature_test.go", "package tddtest\n// red test\n", "test: add red")

	snap, err := snapshotTestFiles(context.Background(), dir, base)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// IMPLEMENTER weakens the test (rewrites the file).
	if err := os.WriteFile(filepath.Join(dir, "feature_test.go"), []byte("package tddtest\n// weakened\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	err = verifyTestFreeze(snap, dir)
	if err == nil || !strings.Contains(err.Error(), reasonTDDTestsModified) {
		t.Fatalf("verify after modification = %v, want %s", err, reasonTDDTestsModified)
	}
	if !strings.Contains(err.Error(), "feature_test.go") {
		t.Errorf("error should name the changed file: %v", err)
	}
}

// TestVerifyDetectsDeletion: deleting the frozen test file fails the guard.
func TestVerifyDetectsDeletion(t *testing.T) {
	dir, base := tddGitRepo(t)
	writeAndCommit(t, dir, "feature_test.go", "package tddtest\n", "test: add red")

	snap, err := snapshotTestFiles(context.Background(), dir, base)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "feature_test.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	err = verifyTestFreeze(snap, dir)
	if err == nil || !strings.Contains(err.Error(), reasonTDDTestsModified) {
		t.Fatalf("verify after deletion = %v, want %s", err, reasonTDDTestsModified)
	}
}

// TestSnapshotIgnoresNonTestFiles: only _test.go files are frozen; production
// files the implementer must edit are not.
func TestSnapshotIgnoresNonTestFiles(t *testing.T) {
	dir, base := tddGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "feature_test.go"), []byte("package tddtest\n"), 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package tddtest\n"), 0o644); err != nil {
		t.Fatalf("write prod: %v", err)
	}
	writeAndCommit(t, dir, "extra.txt", "x\n", "test+prod")

	snap, err := snapshotTestFiles(context.Background(), dir, base)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, ok := snap.Files["feature.go"]; ok {
		t.Error("production feature.go must NOT be frozen")
	}
	if _, ok := snap.Files["feature_test.go"]; !ok {
		t.Error("feature_test.go must be frozen")
	}
}

// TestVerifyTestFreezeNoopOnEmpty: a nil/empty snapshot is a no-op.
func TestVerifyTestFreezeNoopOnEmpty(t *testing.T) {
	if err := verifyTestFreeze(nil, t.TempDir()); err != nil {
		t.Errorf("nil snapshot verify = %v, want nil", err)
	}
	if err := verifyTestFreeze(&testFreezeSnapshot{Files: map[string]string{}}, t.TempDir()); err != nil {
		t.Errorf("empty snapshot verify = %v, want nil", err)
	}
}

// TestChangedTestFilesEmptyBase: an empty base branch yields no files (best-effort).
func TestChangedTestFilesEmptyBase(t *testing.T) {
	files, err := changedTestFiles(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("changedTestFiles empty base: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want none for empty base", files)
	}
}
