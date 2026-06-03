package architect

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// LOCRule flags changed Go files whose line count meets or exceeds the
// project-wide LOCThreshold (400). It reuses the exact line-counter the SCAN
// LOCCollector uses (countLines), reading each changed file from the working
// tree so the count reflects the PR's post-change content.
type LOCRule struct{}

// NewLOCRule returns an LOCRule.
func NewLOCRule() *LOCRule { return &LOCRule{} }

// Name implements Rule.
func (LOCRule) Name() string { return "loc-400" }

// Eval flags every changed *.go file at or over LOCThreshold lines. Files that
// cannot be read (empty worktree, deleted file) are skipped, never errored, so
// the rule is fail-open by construction.
func (LOCRule) Eval(ctx context.Context, changedFiles []string, worktreePath string) []Violation {
	if worktreePath == "" {
		return nil
	}
	var out []Violation
	for _, rel := range changedGoFiles(changedFiles) {
		if err := ctx.Err(); err != nil {
			break
		}
		lines, err := countLines(filepath.Join(worktreePath, rel))
		if err != nil || lines < LOCThreshold {
			continue
		}
		out = append(out, Violation{
			Rule:   "loc-400",
			File:   rel,
			Detail: fmt.Sprintf("file is %d lines (limit %d)", lines, LOCThreshold),
			Risk:   locRisk(lines),
		})
	}
	return out
}

// ForbiddenImportRule flags a changed Go file that introduces a forbidden
// import edge under one of the seeded LayerRules (e.g. internal/executor must
// not import internal/config). It reuses layers.go's prefix matching
// (underPrefix/joinPrefix) so it shares the exact lookalike-safe semantics as
// the whole-graph layer check, but operates file-by-file on a PR's changes
// without needing a full `go list` graph.
type ForbiddenImportRule struct {
	modulePrefix string
	rules        []LayerRule
}

// NewForbiddenImportRule returns a rule scoped to modulePrefix and seeded with
// the given forbidden directions.
func NewForbiddenImportRule(modulePrefix string, rules []LayerRule) *ForbiddenImportRule {
	return &ForbiddenImportRule{modulePrefix: modulePrefix, rules: rules}
}

// Name implements Rule.
func (ForbiddenImportRule) Name() string { return "forbidden-import" }

// Eval parses the imports of each changed Go file, derives the file's own
// package path from its location under worktreePath, and flags any import that
// crosses a forbidden From->To boundary. Unparseable files are skipped.
func (r *ForbiddenImportRule) Eval(ctx context.Context, changedFiles []string, worktreePath string) []Violation {
	if worktreePath == "" {
		return nil
	}
	var out []Violation
	for _, rel := range changedGoFiles(changedFiles) {
		if err := ctx.Err(); err != nil {
			break
		}
		imports := parseImports(filepath.Join(worktreePath, rel))
		if imports == nil {
			continue
		}
		fromPkg := joinPrefix(r.modulePrefix, packageDir(rel))
		out = append(out, r.violationsFor(rel, fromPkg, imports)...)
	}
	return out
}

// violationsFor checks one file's imports against every seeded rule. fromPkg is
// the file's full package import path. For each rule whose From prefix the file
// is under, any import under the rule's To prefix (but not under From, so an
// in-subtree import is allowed) is a violation.
func (r *ForbiddenImportRule) violationsFor(rel, fromPkg string, imports []string) []Violation {
	var out []Violation
	for _, rule := range r.rules {
		from := joinPrefix(r.modulePrefix, rule.From)
		to := joinPrefix(r.modulePrefix, rule.To)
		if !underPrefix(fromPkg, from) {
			continue
		}
		for _, imp := range imports {
			if underPrefix(imp, to) && !underPrefix(imp, from) {
				out = append(out, Violation{
					Rule:   "forbidden-import",
					File:   rel,
					Detail: fmt.Sprintf("%s imports %s (%s)", rule.From, imp, rule.Reason),
					Risk:   pilotapi.RiskHigh,
				})
			}
		}
	}
	return out
}

// parseImports returns the import paths of the Go file at path, or nil if it
// cannot be read or parsed. Only the imports are parsed (parser.ImportsOnly),
// so this is cheap and tolerant of bodies that would not type-check.
func parseImports(path string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		out = append(out, strings.Trim(spec.Path.Value, `"`))
	}
	return out
}

// packageDir returns the directory of a project-relative file path, normalised
// to forward slashes so it composes with module-style import prefixes. A
// top-level file yields "".
func packageDir(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(filepath.ToSlash(rel)))
	if dir == "." {
		return ""
	}
	return dir
}

// isVendoredOrMeta reports whether a project-relative path lives in vendored
// code or VCS metadata and should be excluded from guardrail evaluation.
func isVendoredOrMeta(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, seg := range strings.Split(rel, "/") {
		if shouldSkipDir(seg) {
			return true
		}
	}
	return false
}
