package architect

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubGitRunner returns canned stdout/err for the git diff call.
type stubGitRunner struct {
	out  []byte
	err  error
	dir  string
	args []string
}

func (f *stubGitRunner) run(_ context.Context, dir, _ string, args ...string) ([]byte, error) {
	f.dir = dir
	f.args = args
	return f.out, f.err
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestMissingTestsCollector_Name(t *testing.T) {
	c := NewMissingTestsCollector()
	if c.Name() != "missing_test" {
		t.Fatalf("Name() = %q, want missing_test", c.Name())
	}
}

func TestMissingTestsCollector_FlagsPackageWithoutTest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "untested.go"), "package pkg\n")

	fr := &stubGitRunner{out: []byte("pkg/untested.go\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, err := c.Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 missing_test signal, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "missing_test" || got[0].File != "pkg/untested.go" {
		t.Fatalf("unexpected signal %+v", got[0])
	}
}

func TestMissingTestsCollector_SkipsPackageWithTest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "covered.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "covered_test.go"), "package pkg\n")

	fr := &stubGitRunner{out: []byte("pkg/covered.go\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, err := c.Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("package with a test must not be flagged, got %+v", got)
	}
}

func TestMissingTestsCollector_IgnoresChangedTestFiles(t *testing.T) {
	root := t.TempDir()
	// Only a test file changed; it is not itself a production gap.
	writeTestFile(t, filepath.Join(root, "pkg", "thing_test.go"), "package pkg\n")

	fr := &stubGitRunner{out: []byte("pkg/thing_test.go\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, _ := c.Collect(context.Background(), root)
	if len(got) != 0 {
		t.Fatalf("changed _test.go must not be flagged, got %+v", got)
	}
}

func TestMissingTestsCollector_SkipsDeletedFiles(t *testing.T) {
	root := t.TempDir()
	// File reported by diff but absent on disk (deleted in the range).
	fr := &stubGitRunner{out: []byte("pkg/gone.go\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, _ := c.Collect(context.Background(), root)
	if len(got) != 0 {
		t.Fatalf("deleted file must not be flagged, got %+v", got)
	}
}

func TestMissingTestsCollector_IgnoresNonGoAndBlankLines(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "code.go"), "package pkg\n")

	fr := &stubGitRunner{out: []byte("README.md\n\npkg/code.go\nMakefile\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, _ := c.Collect(context.Background(), root)
	if len(got) != 1 || got[0].File != "pkg/code.go" {
		t.Fatalf("only the changed .go file should flag, got %+v", got)
	}
}

func TestMissingTestsCollector_GitFailureDegrades(t *testing.T) {
	root := t.TempDir()
	fr := &stubGitRunner{err: errors.New("not a git repository")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, err := c.Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("git failure must not surface, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("git failure must yield zero signals, got %+v", got)
	}
}

func TestMissingTestsCollector_EmptyDiff(t *testing.T) {
	fr := &stubGitRunner{out: []byte("")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, _ := c.Collect(context.Background(), t.TempDir())
	if len(got) != 0 {
		t.Fatalf("empty diff => no signals, got %+v", got)
	}
}

func TestMissingTestsCollector_DedupsRepeatedFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "dup.go"), "package pkg\n")
	// Same file listed twice across the range.
	fr := &stubGitRunner{out: []byte("pkg/dup.go\npkg/dup.go\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	got, _ := c.Collect(context.Background(), root)
	if len(got) != 1 {
		t.Fatalf("duplicate paths must collapse to one signal, got %d", len(got))
	}
}

func TestMissingTestsCollector_DefaultRangeFallback(t *testing.T) {
	c := newMissingTestsCollector((&stubGitRunner{}).run, "  ")
	if c.revs != defaultMissingTestRange {
		t.Fatalf("blank range must fall back to %q, got %q", defaultMissingTestRange, c.revs)
	}
}

func TestMissingTestsCollector_PassesRangeToGit(t *testing.T) {
	fr := &stubGitRunner{out: []byte("")}
	c := newMissingTestsCollector(fr.run, "v1.0..HEAD")
	_, _ = c.Collect(context.Background(), t.TempDir())
	joined := strings.Join(fr.args, " ")
	if !strings.Contains(joined, "diff") || !strings.Contains(joined, "--name-only") || !strings.Contains(joined, "v1.0..HEAD") {
		t.Fatalf("git args missing expected diff range: %v", fr.args)
	}
}

func TestMissingTestsCollector_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "a.go"), "package pkg\n")
	fr := &stubGitRunner{out: []byte("pkg/a.go\n")}
	c := newMissingTestsCollector(fr.run, "HEAD~1..HEAD")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := c.Collect(ctx, root)
	if err != nil {
		t.Fatalf("cancellation must not error, got %v", err)
	}
	// The diff still ran (mock ignores ctx), but the per-file loop breaks early.
	if len(got) != 0 {
		t.Fatalf("cancelled context should short-circuit the loop, got %+v", got)
	}
}

// TestMissingTestsCollector_RealGitFixture exercises the production execCommandRunner
// against a real temporary git repository with two commits, proving the
// end-to-end diff + sibling-test detection without any mocks.
func TestMissingTestsCollector_RealGitFixture(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)

	// Commit 1: a tested package and an empty baseline.
	writeTestFile(t, filepath.Join(root, "tested", "t.go"), "package tested\n")
	writeTestFile(t, filepath.Join(root, "tested", "t_test.go"), "package tested\n")
	gitCommitAll(t, root, "base")

	// Commit 2: add a new untested file and touch the tested package.
	writeTestFile(t, filepath.Join(root, "fresh", "new.go"), "package fresh\n")
	writeTestFile(t, filepath.Join(root, "tested", "t.go"), "package tested\n// touched\n")
	gitCommitAll(t, root, "work")

	c := newMissingTestsCollector(execCommandRunner, "HEAD~1..HEAD")
	got, err := c.Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly the untested fresh file, got %d: %+v", len(got), got)
	}
	if got[0].File != "fresh/new.go" {
		t.Fatalf("flagged wrong file: %+v", got[0])
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "tester")
	runGit(t, dir, "config", "commit.gpgsign", "false")
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", msg)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
