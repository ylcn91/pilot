package architect

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// kindMissingTest is the Signal kind the missing-tests collector emits: a
// recently-changed source file whose package has no test coverage at all
// (no sibling _test.go), i.e. a freshly-introduced or freshly-modified test gap.
const kindMissingTest = "missing_test"

// defaultMissingTestRange is the git revision range scanned for changed files
// when the caller does not override it. HEAD~10..HEAD captures the recent burst
// of work most likely to have outrun its tests without walking the whole
// history.
const defaultMissingTestRange = "HEAD~10..HEAD"

// MissingTestsCollector flags recently-changed Go source files that live in a
// package with no test file. It shells `git diff --name-only` over a recent
// commit range, then checks each changed non-test *.go file's directory for any
// *_test.go sibling. A changed file whose package has zero tests is emitted as a
// missing_test Signal.
//
// It is best-effort: outside a git repo, or when git is unavailable, the diff
// yields nothing and the collector returns zero Signals and no error. Files that
// no longer exist on disk (deleted in the range) are skipped.
type MissingTestsCollector struct {
	run   commandRunner
	revs  string
	stat  func(string) (os.FileInfo, error)
	files func(dir string) ([]string, error)
}

// NewMissingTestsCollector returns a MissingTestsCollector wired to the real
// `git` toolchain over the default recent-commit range.
func NewMissingTestsCollector() *MissingTestsCollector {
	return newMissingTestsCollector(execCommandRunner, defaultMissingTestRange)
}

// newMissingTestsCollector is the injectable constructor used by tests: it lets
// a test supply a mock commandRunner and a custom revision range.
func newMissingTestsCollector(run commandRunner, revs string) *MissingTestsCollector {
	if strings.TrimSpace(revs) == "" {
		revs = defaultMissingTestRange
	}
	return &MissingTestsCollector{
		run:   run,
		revs:  revs,
		stat:  os.Stat,
		files: readDirNames,
	}
}

// Name implements Collector.
func (c *MissingTestsCollector) Name() string { return kindMissingTest }

// Collect lists recently-changed Go files, groups them by package directory,
// and emits a missing_test Signal for each changed non-test file whose package
// has no *_test.go sibling. Best-effort: a git failure, an empty diff, or a
// non-repo directory all yield zero Signals and a nil error.
func (c *MissingTestsCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	changed := c.changedGoFiles(ctx, projectPath)
	if len(changed) == 0 {
		return nil, nil
	}

	// Cache per-directory "has any test file" so we stat each package once.
	hasTest := map[string]bool{}
	var signals []Signal

	for _, rel := range changed {
		if err := ctx.Err(); err != nil {
			break
		}
		if !isProductionGoFile(rel) {
			continue
		}
		abs := filepath.Join(projectPath, rel)
		if _, err := c.stat(abs); err != nil {
			// File was deleted in the range (or is unreadable): not a test gap.
			continue
		}

		dir := filepath.Dir(abs)
		covered, seen := hasTest[dir]
		if !seen {
			covered = c.dirHasTest(dir)
			hasTest[dir] = covered
		}
		if covered {
			continue
		}
		signals = append(signals, Signal{
			Kind:   kindMissingTest,
			File:   filepath.ToSlash(rel),
			Line:   0,
			Detail: "changed file in a package with no _test.go (0% coverage)",
			Weight: 1,
			Risk:   pilotapi.RiskMedium,
		})
	}
	return signals, nil
}

// changedGoFiles returns the project-relative paths of *.go files that changed
// in the configured revision range. A git failure yields nil (best-effort).
func (c *MissingTestsCollector) changedGoFiles(ctx context.Context, projectPath string) []string {
	out, err := c.run(ctx, projectPath, "git", "diff", "--name-only", c.revs)
	if err != nil && len(out) == 0 {
		return nil
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		p := strings.TrimSpace(line)
		if p == "" || !strings.HasSuffix(p, ".go") {
			continue
		}
		files = append(files, p)
	}
	sort.Strings(files)
	return dedupStrings(files)
}

// dirHasTest reports whether dir contains any *_test.go file. A read error is
// treated as "no tests" so a missing/unreadable directory still surfaces the
// gap rather than silently hiding it.
func (c *MissingTestsCollector) dirHasTest(dir string) bool {
	names, err := c.files(dir)
	if err != nil {
		return false
	}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			return true
		}
	}
	return false
}

// isProductionGoFile reports whether rel is a non-test Go source file (a *.go
// that is not itself a *_test.go). Only production files create a test gap.
func isProductionGoFile(rel string) bool {
	return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
}

// readDirNames lists the base names of the entries directly under dir.
func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// dedupStrings removes consecutive duplicates from a sorted slice in place,
// returning the deduplicated prefix.
func dedupStrings(sorted []string) []string {
	if len(sorted) < 2 {
		return sorted
	}
	out := sorted[:1]
	for _, s := range sorted[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
