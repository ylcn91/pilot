package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// reasonTDDTestsModified fails the run when the IMPLEMENTER changed, weakened, or
// deleted the test files committed by the TEST-AUTHOR. The RED tests are the
// proof of intent; the implementer must make them pass AS WRITTEN, never edit
// them. This is enforced via the TEST-FREEZE guard (content-hash snapshot).
const reasonTDDTestsModified = "tdd_tests_modified"

// reasonTDDBaselineNotGreen aborts the run when the suite is already RED BEFORE
// the TEST-AUTHOR writes any tests. A pre-existing failing suite would make the
// later RED gate spuriously red (the new tests' failure could not be attributed
// to the new behavior), so we refuse to proceed.
const reasonTDDBaselineNotGreen = "tdd_baseline_not_green"

// testFreezeSnapshot records the content hashes of the test files the TEST-AUTHOR
// committed, so the TEST-FREEZE guard can detect any post-commit modification or
// deletion by the IMPLEMENTER. Files maps a repo-relative path to a sha256 hex
// digest. An empty/nil snapshot means there was nothing to freeze (best-effort:
// non-Go projects or a test-author that emitted no test-file diff).
type testFreezeSnapshot struct {
	Files map[string]string
}

// snapshotTestFiles builds a TEST-FREEZE snapshot from the test files changed on
// the current branch relative to baseBranch. It hashes the on-disk content of
// each `_test.go` file (Go) that the TEST-AUTHOR added/modified. Files that were
// listed in the diff but no longer exist on disk are recorded with the sentinel
// "<deleted>" so a later re-creation with different content is still caught.
func snapshotTestFiles(ctx context.Context, projectPath, baseBranch string) (*testFreezeSnapshot, error) {
	paths, err := changedTestFiles(ctx, projectPath, baseBranch)
	if err != nil {
		return nil, err
	}
	snap := &testFreezeSnapshot{Files: make(map[string]string, len(paths))}
	for _, p := range paths {
		h, herr := hashFileIfExists(filepath.Join(projectPath, p))
		if herr != nil {
			return nil, herr
		}
		snap.Files[p] = h
	}
	return snap, nil
}

// verifyTestFreeze re-hashes the snapshotted test files and returns an error
// (reasonTDDTestsModified) if any file's content changed or it was deleted since
// the snapshot. A nil/empty snapshot is a no-op (nothing was frozen).
func verifyTestFreeze(snap *testFreezeSnapshot, projectPath string) error {
	if snap == nil || len(snap.Files) == 0 {
		return nil
	}
	var changed []string
	for p, want := range snap.Files {
		got, err := hashFileIfExists(filepath.Join(projectPath, p))
		if err != nil {
			return err
		}
		if got != want {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	sort.Strings(changed)
	return fmt.Errorf("%s: IMPLEMENTER modified or deleted frozen test file(s): %s — the RED tests must pass as written, not be edited",
		reasonTDDTestsModified, strings.Join(changed, ", "))
}

// changedTestFiles returns the repo-relative `_test.go` paths changed on the
// current branch relative to baseBranch. It uses `git diff --name-only
// base...HEAD` (three-dot: only commits on this branch) and filters to Go test
// files. A missing base branch yields an empty list (best-effort).
func changedTestFiles(ctx context.Context, projectPath, baseBranch string) ([]string, error) {
	if baseBranch == "" {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", baseBranch+"...HEAD")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tdd freeze: git diff failed: %w", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "_test.go") {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return files, nil
}

// hashFileIfExists returns the sha256 hex digest of the file content, or the
// sentinel "<deleted>" when the file does not exist (so deletion is detectable).
func hashFileIfExists(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "<deleted>", nil
		}
		return "", fmt.Errorf("tdd freeze: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
