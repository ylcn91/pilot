package architect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// maxFilesPerFinding caps how many file/location entries a synthesized Finding
// lists. A deterministic offline finding summarises a whole cluster of Signals;
// listing every member would bloat the issue body without adding signal, so the
// tail is collapsed into the WhyItMatters count instead.
const maxFilesPerFinding = 12

// maxPRPiecesPerFinding caps the suggested-PR-piece list of a synthesized
// Finding for the same reason: a handful of concrete, reviewable steps beats an
// exhaustive per-file enumeration.
const maxPRPiecesPerFinding = 6

// kindMeta describes how a Signal Kind is rendered into a Finding when the
// deterministic synthesizer runs (the offline PROPOSE path). It supplies the
// human framing the LLM would otherwise generate, derived entirely from the
// Signal's category so the output stays reproducible and network-free.
type kindMeta struct {
	// findingKind is the pilotapi.Finding.Kind assigned to the cluster.
	findingKind string
	// title templates the Finding title; "%d" is the cluster size.
	titleTmpl string
	// why explains, in one sentence, why the cluster matters.
	why string
	// pieceTmpl templates one suggested PR piece per Signal; "%s" is the
	// Signal's location (File[:Line]). Empty falls back to a generic piece.
	pieceTmpl string
}

// kindCatalog maps the deterministic SCAN Signal kinds onto the framing the
// offline synthesizer uses. A Kind absent from the catalog still produces a
// usable Finding via the generic fallback in metaFor, so a new collector kind is
// never silently dropped.
var kindCatalog = map[string]kindMeta{
	kindImportCycle: {
		findingKind: "refactor",
		titleTmpl:   "Break %d package import cycle(s)",
		why:         "Import cycles block clean layering and incremental builds; each strongly connected package group must be broken to restore a build-friendly DAG.",
		pieceTmpl:   "Break the cycle reported at %s by extracting the shared types into a lower-level package",
	},
	kindLayerViolation: {
		findingKind: "hardening",
		titleTmpl:   "Fix %d architectural layer violation(s)",
		why:         "Forbidden cross-layer imports invert the dependency direction the codebase already satisfies elsewhere; each is a structural regression, not a style nit.",
		pieceTmpl:   "Remove the forbidden import originating from %s, routing through the intended layer instead",
	},
	kindHeavyDep: {
		findingKind: "refactor",
		titleTmpl:   "Review %d heavily-depended-upon module(s)",
		why:         "Modules with high module-graph fan-in are broadly entrenched: an upgrade, swap, or breaking change ripples across many packages, so they warrant a deliberate isolation plan.",
		pieceTmpl:   "Audit the blast radius of %s and gate it behind a narrow internal seam",
	},
	kindUnusedDep: {
		findingKind: "refactor",
		titleTmpl:   "Drop %d unused dependency/dependencies",
		why:         "Required-but-unimported modules are dead weight in go.mod: they slow resolution, widen the supply-chain surface, and mislead readers about what the project actually uses.",
		pieceTmpl:   "Remove %s from go.mod (go mod tidy) after confirming no build/test references it",
	},
	"loc_over_400": {
		findingKind: "refactor",
		titleTmpl:   "Split %d oversized file(s) under the LOC threshold",
		why:         "Files past the line threshold concentrate too much responsibility in one unit, making review, testing, and safe change harder; each should be decomposed into focused siblings.",
		pieceTmpl:   "Decompose %s into smaller, single-responsibility files",
	},
	"todo_fixme": {
		findingKind: "hardening",
		titleTmpl:   "Resolve %d TODO/FIXME marker(s)",
		why:         "Lingering TODO/FIXME markers record known, unfinished work; triaging them either closes a real gap or removes stale noise that erodes trust in the comments.",
		pieceTmpl:   "Resolve or remove the TODO/FIXME at %s",
	},
	"low_coverage": {
		findingKind: "test-gap",
		titleTmpl:   "Raise coverage for %d under-tested package(s)",
		why:         "Packages below the coverage threshold ship behaviour no test exercises, so regressions land silently; targeted tests on the uncovered paths close the gap.",
		pieceTmpl:   "Add tests covering the uncovered paths in %s",
	},
	"missing_test": {
		findingKind: "test-gap",
		titleTmpl:   "Add tests for %d recently-changed untested file(s)",
		why:         "Recently-changed files with no test at all are the highest-risk coverage gap: fresh logic with zero guardrails. Each needs at least a happy-path and edge-case test.",
		pieceTmpl:   "Add unit and edge-case tests for %s",
	},
	"bug_hotspot": {
		findingKind: "test-gap",
		titleTmpl:   "Harden %d recurring bug hotspot(s) with tests",
		why:         "Areas that keep breaking are where regression tests pay off most; pin the failure modes with -race and table-driven tests before the next change reopens them.",
		pieceTmpl:   "Add a regression test reproducing the recurring failure at %s",
	},
	kindDuplicateBlock: {
		findingKind: "refactor",
		titleTmpl:   "Consolidate %d duplicated code block(s)",
		why:         "Copy-pasted blocks drift apart over time: a fix applied to one copy silently misses the others, so the same bug reappears and behaviour diverges across call sites. Extracting the shared block into one helper makes each future change land in a single place.",
		pieceTmpl:   "Extract the duplicated block at %s into a shared helper and replace the copies",
	},
	KindRuleSuggestion: {
		findingKind: "hardening",
		titleTmpl:   "Review %d candidate guardrail rule(s)",
		why:         "The deterministic scan mined recurring forbidden-import boundaries from recorded pitfalls/decisions and repeated violations. Each is an ADVISORY draft, never an enforced rule: a human reviews the draft and, if sound, promotes it by adding it to defaultLayerRules.",
		pieceTmpl:   "Review the draft guardrail rule at %s and promote it to defaultLayerRules if the boundary is real",
	},
	KnownDecisionKind: {
		findingKind: "hardening",
		titleTmpl:   "Reconcile %d recorded architectural decision(s)",
		why:         "Recorded architectural decisions capture boundaries the codebase committed to; surfacing them keeps current work from silently drifting away from a deliberate choice.",
		pieceTmpl:   "Confirm the code at %s still honours the recorded architectural decision",
	},
}

// metaFor returns the framing for a Signal kind, falling back to a generic but
// still-useful descriptor for any kind not in the catalog so no collector's
// output is silently dropped from the offline path.
func metaFor(kind string) kindMeta {
	if m, ok := kindCatalog[kind]; ok {
		return m
	}
	pretty := strings.ReplaceAll(kind, "_", " ")
	return kindMeta{
		findingKind: "refactor",
		titleTmpl:   "Address %d " + pretty + " signal(s)",
		why:         "The deterministic scan flagged " + pretty + " signals; each marks a concrete improvement opportunity worth triaging.",
		pieceTmpl:   "Address the " + pretty + " signal at %s",
	}
}

// SynthesizeFindings turns deterministic SCAN Signals directly into ranked
// pilotapi.Finding proposals without an LLM. It is the offline PROPOSE path:
// every Finding is derived solely from the Signals, so identical Signals always
// produce identical, network-free output.
//
// Signals are grouped by Kind (the cluster), and each non-empty group becomes
// one Finding whose Risk is the group's maximum, whose Files are the distinct
// locations (sorted, capped), and whose suggested_pr_pieces are deterministic
// per-location steps. Groups are emitted in descending Risk then ascending Kind
// order, so the result is stable across runs.
func SynthesizeFindings(signals []Signal) []pilotapi.Finding {
	groups := groupByKind(signals)
	if len(groups) == 0 {
		return []pilotapi.Finding{}
	}

	out := make([]pilotapi.Finding, 0, len(groups))
	for _, kind := range sortedGroupKinds(groups) {
		if f, ok := synthesizeGroup(kind, groups[kind]); ok {
			out = append(out, f)
		}
	}
	return out
}

// synthesizeGroup builds one Finding from a single Kind's Signals. It returns
// ok=false only for an empty group (which sortedGroupKinds never yields), so the
// caller can append unconditionally.
func synthesizeGroup(kind string, group []Signal) (pilotapi.Finding, bool) {
	if len(group) == 0 {
		return pilotapi.Finding{}, false
	}
	meta := metaFor(kind)

	locs := distinctLocations(group)
	risk := maxRisk(group)

	files := locs
	overflow := 0
	if len(files) > maxFilesPerFinding {
		overflow = len(files) - maxFilesPerFinding
		files = files[:maxFilesPerFinding]
	}

	why := meta.why
	if overflow > 0 {
		why += fmt.Sprintf(" (%d related location(s) not listed individually)", overflow)
	}

	return pilotapi.Finding{
		Title:             fmt.Sprintf(meta.titleTmpl, len(locs)),
		Kind:              meta.findingKind,
		Risk:              risk,
		WhyItMatters:      why,
		SuggestedPRPieces: prPieces(meta, locs),
		TestPlan:          testPlanFor(meta.findingKind),
		Files:             files,
	}, true
}

// prPieces renders up to maxPRPiecesPerFinding deterministic suggested PR steps,
// one per location, collapsing any tail into a single aggregate step so the list
// stays reviewable.
func prPieces(meta kindMeta, locs []string) []string {
	limit := len(locs)
	tail := 0
	if limit > maxPRPiecesPerFinding {
		tail = limit - maxPRPiecesPerFinding
		limit = maxPRPiecesPerFinding
	}
	pieces := make([]string, 0, limit+1)
	for _, loc := range locs[:limit] {
		pieces = append(pieces, fmt.Sprintf(meta.pieceTmpl, loc))
	}
	if tail > 0 {
		pieces = append(pieces, fmt.Sprintf("Apply the same fix to the remaining %d location(s)", tail))
	}
	return pieces
}

// testPlanFor returns a deterministic verification plan keyed off the finding
// kind so a test-gap finding's plan reads about coverage while a refactor's reads
// about behaviour preservation.
func testPlanFor(findingKind string) string {
	switch findingKind {
	case "test-gap":
		return "Add the proposed tests, then run `go test ./... -race` and confirm the new cases fail before the fix and pass after; verify coverage rises for the targeted packages."
	case "hardening":
		return "Run `go build ./...` and `go test ./...` after each change to confirm the structural fix compiles and preserves behaviour; add a guard test where the violation could recur."
	default:
		return "Run `go build ./...` and `go test ./...` before and after each step to confirm behaviour is preserved while the structure changes."
	}
}

// groupByKind buckets Signals by their Kind. Empty-kind Signals are skipped:
// a Signal with no Kind cannot be framed and would otherwise produce a
// meaningless "address 0 signal(s)" finding.
func groupByKind(signals []Signal) map[string][]Signal {
	groups := make(map[string][]Signal)
	for _, s := range signals {
		k := strings.TrimSpace(s.Kind)
		if k == "" {
			continue
		}
		groups[k] = append(groups[k], s)
	}
	return groups
}

// sortedGroupKinds orders the group keys for deterministic emission: descending
// by the group's maximum risk, then ascending by kind name as the tiebreaker.
func sortedGroupKinds(groups map[string][]Signal) []string {
	kinds := make([]string, 0, len(groups))
	for k := range groups {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		ri := riskRank(maxRisk(groups[kinds[i]]))
		rj := riskRank(maxRisk(groups[kinds[j]]))
		if ri != rj {
			return ri > rj
		}
		return kinds[i] < kinds[j]
	})
	return kinds
}

// distinctLocations returns the sorted, de-duplicated set of Signal locations in
// a group. A location is File[:Line] when a line is set, else File; a Signal
// with no File is skipped (it carries no actionable target).
func distinctLocations(group []Signal) []string {
	seen := make(map[string]bool, len(group))
	locs := make([]string, 0, len(group))
	for _, s := range group {
		loc := signalLocation(s)
		if loc == "" || seen[loc] {
			continue
		}
		seen[loc] = true
		locs = append(locs, loc)
	}
	sort.Strings(locs)
	return locs
}

// signalLocation renders a Signal's location as File[:Line], or "" when the
// Signal carries no File.
func signalLocation(s Signal) string {
	file := strings.TrimSpace(s.File)
	if file == "" {
		return ""
	}
	if s.Line > 0 {
		return fmt.Sprintf("%s:%d", file, s.Line)
	}
	return file
}

// maxRisk returns the highest-severity Risk in a group, defaulting to medium for
// a group whose Signals all carry an empty/unknown risk so the Finding always
// has a canonical level.
func maxRisk(group []Signal) pilotapi.RiskLevel {
	best := pilotapi.RiskLevel("")
	bestRank := -1
	for _, s := range group {
		r := normalizeRisk(s.Risk)
		if rank := riskRank(r); rank > bestRank {
			bestRank = rank
			best = r
		}
	}
	if best == "" {
		return pilotapi.RiskMedium
	}
	return best
}
