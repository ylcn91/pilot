package architect

import (
	"context"
	"sort"
	"strings"
)

// OwnershipCollector resolves a best-effort top-author-per-file map by shelling
// `git log`. It is NOT a Signal Collector (it does not feed the SCAN spine);
// it is a side helper the refactor-planner lens uses to annotate each proposed
// PR with the file's likely reviewer/owner. Kept here next to the other
// git-shelling collectors so it reuses the same commandRunner injection point.
//
// Every path is best-effort: a missing `git` binary, a non-git directory, an
// untracked file, or unparseable output yields no owner for that file (an empty
// string) rather than an error, so ownership annotation degrades gracefully to
// "unknown" without ever aborting a plan.
type OwnershipCollector struct {
	run commandRunner
}

// NewOwnershipCollector returns an OwnershipCollector wired to the real `git`
// toolchain.
func NewOwnershipCollector() *OwnershipCollector {
	return newOwnershipCollector(execCommandRunner)
}

// newOwnershipCollector is the injectable constructor used by tests: it lets a
// test supply a mock commandRunner so no test ever spawns `git`.
func newOwnershipCollector(run commandRunner) *OwnershipCollector {
	return &OwnershipCollector{run: run}
}

// TopAuthor returns the author who has authored the most commits touching path
// (relative to projectPath), or "" when git is unavailable, the file is
// untracked, or nothing could be parsed. The result is deterministic: ties are
// broken by author name so the same history always yields the same owner.
func (c *OwnershipCollector) TopAuthor(ctx context.Context, projectPath, path string) string {
	if c == nil || c.run == nil || strings.TrimSpace(path) == "" {
		return ""
	}
	out, err := c.run(ctx, projectPath, "git", "log", "--no-merges", "--format=%an", "--", path)
	if err != nil && len(out) == 0 {
		return ""
	}
	return topAuthorFromLog(string(out))
}

// Owners resolves the top author for each of paths, returning a map keyed by the
// input path. Paths with no resolvable owner are omitted, so an empty result is
// the correct degraded answer (no git / no history) rather than a map of empty
// strings. The probe is per-path and best-effort; a single failing path never
// affects the others.
func (c *OwnershipCollector) Owners(ctx context.Context, projectPath string, paths []string) map[string]string {
	owners := make(map[string]string, len(paths))
	if c == nil || c.run == nil {
		return owners
	}
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if err := ctx.Err(); err != nil {
			break
		}
		if owner := c.TopAuthor(ctx, projectPath, p); owner != "" {
			owners[p] = owner
		}
	}
	return owners
}

// topAuthorFromLog tallies author lines from `git log --format=%an` output and
// returns the most frequent author. Blank lines (which `git log` never emits for
// %an but a malformed stream might) are ignored. Ties are broken
// lexicographically so the result is reproducible.
func topAuthorFromLog(log string) string {
	counts := make(map[string]int)
	for _, line := range strings.Split(log, "\n") {
		author := strings.TrimSpace(line)
		if author == "" {
			continue
		}
		counts[author]++
	}
	if len(counts) == 0 {
		return ""
	}

	authors := make([]string, 0, len(counts))
	for a := range counts {
		authors = append(authors, a)
	}
	sort.Slice(authors, func(i, j int) bool {
		if counts[authors[i]] != counts[authors[j]] {
			return counts[authors[i]] > counts[authors[j]]
		}
		return authors[i] < authors[j]
	})
	return authors[0]
}
