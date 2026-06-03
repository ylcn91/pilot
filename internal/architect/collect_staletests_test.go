package architect

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// timeRunner is a mock commandRunner that returns a per-path commit time (as
// `%ct` seconds) for `git log` calls, simulating git history without a real
// repo. A path absent from times yields an error+empty output, exercising the
// mtime fallback. fail forces every call to error.
type timeRunner struct {
	times map[string]int64
	fail  bool
	calls int
}

func (r *timeRunner) run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	r.calls++
	if r.fail || name != "git" {
		return nil, errors.New("git unavailable")
	}
	// args: log -1 --format=%ct -- <path>
	path := args[len(args)-1]
	for p, secs := range r.times {
		if filepath.Base(p) == filepath.Base(path) {
			return []byte(strconv.FormatInt(secs, 10) + "\n"), nil
		}
	}
	return nil, errors.New("untracked")
}

func newStaleTestsWithTimes(r *timeRunner) *StaleTestsCollector {
	c := newStaleTestsCollector(r.run)
	return c
}

func TestStaleTestsCollector_Name(t *testing.T) {
	if c := NewStaleTestsCollector(); c.Name() != "stale_test" {
		t.Fatalf("Name() = %q, want stale_test", c.Name())
	}
}

func TestSourceForTest(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/p/pkg/foo_test.go", "/p/pkg/foo.go"},
		{"/p/pkg/foo.go", ""},   // not a test file
		{"/p/pkg/_test.go", ""}, // empty stem
		{"/p/pkg/handler_test.go", "/p/pkg/handler.go"},
		{"bar_test.go", "bar.go"},
	}
	for _, tt := range tests {
		if got := sourceForTest(tt.in); filepath.ToSlash(got) != filepath.ToSlash(tt.want) {
			t.Errorf("sourceForTest(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStaleTestsCollector_FlagsStaleTest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "foo.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "foo_test.go"), "package pkg\n")

	// Source changed at t=2000, test at t=1000 -> source leads -> stale.
	r := &timeRunner{times: map[string]int64{
		"foo.go":      2000,
		"foo_test.go": 1000,
	}}
	c := newStaleTestsWithTimes(r)
	got, err := c.Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 stale_test signal, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "stale_test" || got[0].File != "pkg/foo_test.go" {
		t.Fatalf("unexpected signal %+v", got[0])
	}
	if got[0].Risk != "medium" {
		t.Errorf("stale_test risk = %q, want medium", got[0].Risk)
	}
}

func TestStaleTestsCollector_UpToDateNotFlagged(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "foo.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "foo_test.go"), "package pkg\n")

	// Test newer than source -> not stale.
	r := &timeRunner{times: map[string]int64{
		"foo.go":      1000,
		"foo_test.go": 2000,
	}}
	got, _ := newStaleTestsWithTimes(r).Collect(context.Background(), root)
	if len(got) != 0 {
		t.Fatalf("up-to-date test must not flag, got %+v", got)
	}
}

func TestStaleTestsCollector_SameCommitNotFlagged(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "foo.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "foo_test.go"), "package pkg\n")

	// Identical times (committed together) -> gap below threshold -> not flagged.
	r := &timeRunner{times: map[string]int64{
		"foo.go":      1500,
		"foo_test.go": 1500,
	}}
	got, _ := newStaleTestsWithTimes(r).Collect(context.Background(), root)
	if len(got) != 0 {
		t.Fatalf("same-commit test must not flag, got %+v", got)
	}
}

func TestStaleTestsCollector_NoSourceSiblingSkipped(t *testing.T) {
	root := t.TempDir()
	// A _test.go with no matching source file (foo.go absent).
	writeTestFile(t, filepath.Join(root, "pkg", "foo_test.go"), "package pkg\n")

	r := &timeRunner{times: map[string]int64{"foo_test.go": 2000}}
	got, _ := newStaleTestsWithTimes(r).Collect(context.Background(), root)
	if len(got) != 0 {
		t.Fatalf("orphan test (no source) must not flag, got %+v", got)
	}
}

func TestStaleTestsCollector_GitAbsentFallsBackToMtime(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "pkg", "foo.go")
	testPath := filepath.Join(root, "pkg", "foo_test.go")
	writeTestFile(t, srcPath, "package pkg\n")
	writeTestFile(t, testPath, "package pkg\n")

	// Make the test file old and the source new on disk; git always fails so the
	// collector must fall back to mtime and still detect staleness.
	old := time.Now().Add(-2 * time.Hour)
	now := time.Now()
	if err := os.Chtimes(testPath, old, old); err != nil {
		t.Fatalf("chtimes test: %v", err)
	}
	if err := os.Chtimes(srcPath, now, now); err != nil {
		t.Fatalf("chtimes src: %v", err)
	}

	r := &timeRunner{fail: true}
	got, _ := newStaleTestsWithTimes(r).Collect(context.Background(), root)
	if len(got) != 1 {
		t.Fatalf("mtime fallback should detect staleness, got %d: %+v", len(got), got)
	}
	if r.calls == 0 {
		t.Fatal("expected git to be attempted before mtime fallback")
	}
}

func TestStaleTestsCollector_EmptyProjectNoSignals(t *testing.T) {
	got, err := NewStaleTestsCollector().Collect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("empty project must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty project => no signals, got %+v", got)
	}
}

func TestStaleTestsCollector_UnwalkableRootDegrades(t *testing.T) {
	c := NewStaleTestsCollector()
	c.walk = func(string) ([]string, error) { return nil, errors.New("boom") }
	got, err := c.Collect(context.Background(), "/anything")
	if err != nil {
		t.Fatalf("walk error must not surface: %v", err)
	}
	if got != nil {
		t.Fatalf("walk error => nil signals, got %+v", got)
	}
}

func TestStaleTestsCollector_SkipsVendorAndHidden(t *testing.T) {
	root := t.TempDir()
	// A stale test buried under vendor/ and another under .hidden/ must be pruned.
	writeTestFile(t, filepath.Join(root, "vendor", "v.go"), "package v\n")
	writeTestFile(t, filepath.Join(root, "vendor", "v_test.go"), "package v\n")
	writeTestFile(t, filepath.Join(root, ".hidden", "h.go"), "package h\n")
	writeTestFile(t, filepath.Join(root, ".hidden", "h_test.go"), "package h\n")
	// A real, top-level stale test that must still be found.
	writeTestFile(t, filepath.Join(root, "pkg", "real.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "real_test.go"), "package pkg\n")

	r := &timeRunner{times: map[string]int64{
		"v.go": 9000, "v_test.go": 1, "h.go": 9000, "h_test.go": 1,
		"real.go": 9000, "real_test.go": 1,
	}}
	got, _ := newStaleTestsWithTimes(r).Collect(context.Background(), root)
	if len(got) != 1 || got[0].File != "pkg/real_test.go" {
		t.Fatalf("must skip vendor/hidden and flag only pkg/real_test.go, got %+v", got)
	}
}

func TestStaleTestsCollector_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "foo.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "foo_test.go"), "package pkg\n")

	r := &timeRunner{times: map[string]int64{"foo.go": 2000, "foo_test.go": 1000}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := newStaleTestsWithTimes(r).Collect(ctx, root)
	if err != nil {
		t.Fatalf("cancellation must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cancelled context should short-circuit, got %+v", got)
	}
}

func TestStaleTestsCollector_GitCommitTimeUnparseable(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "foo.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "foo_test.go"), "package pkg\n")

	// Runner returns garbage (non-numeric) for git log -> must fall back to mtime,
	// which here is identical for both files -> no stale flag.
	bad := func(_ context.Context, _ string, name string, _ ...string) ([]byte, error) {
		return []byte("not-a-number\n"), nil
	}
	c := newStaleTestsCollector(bad)
	got, _ := c.Collect(context.Background(), root)
	if len(got) != 0 {
		t.Fatalf("unparseable git time should degrade to mtime, got %+v", got)
	}
}

// TestStaleTestsCollector_RealGitFixture exercises the production
// execCommandRunner against a real git repo: a test committed first, then its
// source touched in a later commit, so the source's commit time leads the
// test's and the file is flagged stale end-to-end without mocks.
func TestStaleTestsCollector_RealGitFixture(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)

	srcRel := filepath.Join("pkg", "widget.go")
	testRel := filepath.Join("pkg", "widget_test.go")
	writeTestFile(t, filepath.Join(root, srcRel), "package pkg\n")
	writeTestFile(t, filepath.Join(root, testRel), "package pkg\n")
	gitCommitAll(t, root, "initial: source + test together")

	// Backdate the test's last commit by rewriting only the source in a newer
	// commit a couple of seconds later, so the source's %ct leads the test's.
	time.Sleep(1100 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, srcRel), "package pkg\n// behaviour changed\n")
	gitCommitAll(t, root, "change source only")

	c := NewStaleTestsCollector()
	got, err := c.Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the stale widget test flagged, got %d: %+v", len(got), got)
	}
	if got[0].File != "pkg/widget_test.go" {
		t.Fatalf("flagged wrong file: %+v", got[0])
	}
}

func TestStaleTestsCollector_RealGitFixture_UpToDateNotFlagged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)

	srcRel := filepath.Join("pkg", "calc.go")
	testRel := filepath.Join("pkg", "calc_test.go")
	writeTestFile(t, filepath.Join(root, srcRel), "package pkg\n")
	gitCommitAll(t, root, "source first")

	time.Sleep(1100 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, testRel), "package pkg\n")
	gitCommitAll(t, root, "test second (newer)")

	got, err := NewStaleTestsCollector().Collect(context.Background(), root)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("test newer than source must not flag, got %+v", got)
	}
}
