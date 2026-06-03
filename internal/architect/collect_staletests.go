package architect

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// kindStaleTest is the Signal kind the stale-tests collector emits: a _test.go
// file whose corresponding source file changed more recently, so the test
// likely no longer exercises the current behaviour.
const kindStaleTest = "stale_test"

// staleTestMinGap is the minimum number of seconds the source must lead the
// test by before the test is flagged. A small positive gap avoids flagging
// tests and sources that were committed together (same commit -> identical
// commit time) or saved within the same filesystem-mtime tick.
const staleTestMinGap int64 = 1

// StaleTestsCollector flags `_test.go` files whose corresponding source file
// changed more recently than the test, i.e. tests that probably no longer
// cover the current behaviour. For each test file it derives the sibling source
// file (foo_test.go -> foo.go) and compares the two files' last-change times.
//
// "Last change" is the git commit time of the most recent commit touching the
// file (via `git log -1 --format=%ct -- <path>`); when git is unavailable, the
// repository has no commit for a path, or the directory is not a git repo, it
// falls back to the file's filesystem mtime. The whole collector is
// best-effort: a missing source sibling, an unreadable file, or a git failure
// degrades to "not stale" rather than erroring, so a broken toolchain never
// aborts the scan.
type StaleTestsCollector struct {
	run     commandRunner
	walk    func(root string) ([]string, error)
	modTime func(string) (int64, bool)
	exists  func(string) bool
}

// NewStaleTestsCollector returns a StaleTestsCollector wired to the real `git`
// toolchain and the production file walk.
func NewStaleTestsCollector() *StaleTestsCollector {
	return newStaleTestsCollector(execCommandRunner)
}

// newStaleTestsCollector is the injectable constructor used by tests: it lets a
// test supply a mock commandRunner while keeping the real filesystem probes.
func newStaleTestsCollector(run commandRunner) *StaleTestsCollector {
	return &StaleTestsCollector{
		run:     run,
		walk:    walkTestFiles,
		modTime: fileMTime,
		exists:  fileExists,
	}
}

// Name implements Collector.
func (c *StaleTestsCollector) Name() string { return kindStaleTest }

// Collect walks the project for `_test.go` files and emits a stale_test Signal
// for each one whose sibling source file last changed more recently. Tests with
// no resolvable source sibling, or that are at least as new as their source,
// are not flagged. Best-effort: any per-file probe failure simply skips that
// file. The returned error is always nil.
func (c *StaleTestsCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	testFiles, err := c.walk(projectPath)
	if err != nil {
		// An unwalkable root yields no signals, never an abort.
		return nil, nil
	}
	sort.Strings(testFiles)

	var signals []Signal
	for _, abs := range testFiles {
		if err := ctx.Err(); err != nil {
			break
		}
		src := sourceForTest(abs)
		if src == "" || !c.exists(src) {
			continue
		}

		testTime, okT := c.changeTime(ctx, projectPath, abs)
		srcTime, okS := c.changeTime(ctx, projectPath, src)
		if !okT || !okS {
			continue
		}
		if srcTime-testTime < staleTestMinGap {
			continue
		}

		rel := relProjectPath(projectPath, abs)
		signals = append(signals, Signal{
			Kind:   kindStaleTest,
			File:   rel,
			Line:   0,
			Detail: "test is older than its source file (" + filepath.Base(src) + " changed more recently); coverage may be stale",
			Weight: 1,
			Risk:   pilotapi.RiskMedium,
		})
	}
	return signals, nil
}

// changeTime returns the most recent change time of path in seconds, preferring
// the git commit time and falling back to the filesystem mtime. The boolean is
// false only when neither source yields a time (e.g. the file vanished between
// the walk and the probe).
func (c *StaleTestsCollector) changeTime(ctx context.Context, projectPath, path string) (int64, bool) {
	if t, ok := c.gitCommitTime(ctx, projectPath, path); ok {
		return t, true
	}
	return c.modTime(path)
}

// gitCommitTime returns the unix commit time of the most recent commit touching
// path. It returns ok=false when git is unavailable, the file is untracked, or
// the output is unparseable, so the caller can fall back to mtime.
func (c *StaleTestsCollector) gitCommitTime(ctx context.Context, projectPath, path string) (int64, bool) {
	out, err := c.run(ctx, projectPath, "git", "log", "-1", "--format=%ct", "--", path)
	if err != nil && len(out) == 0 {
		return 0, false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return 0, false
	}
	secs, perr := strconv.ParseInt(line, 10, 64)
	if perr != nil {
		return 0, false
	}
	return secs, true
}

// sourceForTest maps an absolute `_test.go` path to its sibling source file
// (foo_test.go -> foo.go). It returns "" for a path that is not a *_test.go
// file or whose stripped base would be empty.
func sourceForTest(testPath string) string {
	base := filepath.Base(testPath)
	if !strings.HasSuffix(base, "_test.go") {
		return ""
	}
	stem := strings.TrimSuffix(base, "_test.go")
	if stem == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(testPath), stem+".go")
}

// relProjectPath renders abs as a forward-slash path relative to projectPath,
// falling back to the absolute path (slash-normalised) when it is not under the
// root.
func relProjectPath(projectPath, abs string) string {
	rel, err := filepath.Rel(projectPath, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// walkTestFiles returns the absolute paths of every *_test.go file under root,
// skipping hidden directories and vendor so the walk stays fast and stable.
func walkTestFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipWalkDir(root, path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// skipWalkDir reports whether a directory should be pruned from the test-file
// walk: hidden directories (.git, .agent, ...) other than the root itself, and
// vendor/node_modules trees that are never the project's own tests.
func skipWalkDir(root, path, name string) bool {
	if path == root {
		return false
	}
	if name == "vendor" || name == "node_modules" {
		return true
	}
	return strings.HasPrefix(name, ".")
}

// fileMTime returns the filesystem modification time of path in unix seconds.
func fileMTime(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.ModTime().Unix(), true
}

// fileExists reports whether path names an existing file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
