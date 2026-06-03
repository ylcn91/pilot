package architect

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Refactor-plan sizing bounds. The epic machinery (createSubIssuesViaAdapter /
// ExecuteSubIssues) is built for a handful of linked, ordered tickets, not an
// unbounded flood: a plan with fewer than minPlanSubtasks is not worth an epic,
// and one past maxPlanSubtasks overwhelms review. ProjectToEpic caps the
// ordered sequence to [minPlanSubtasks, maxPlanSubtasks].
const (
	minPlanSubtasks = 5
	maxPlanSubtasks = 20
)

// highBlastRadius is the dependent-package count at or above which a change is
// treated as high blast-radius: touching it could ripple into this many other
// packages, so the planner flags it manual-review-required rather than
// pilot-safe.
const highBlastRadius = 5

// Refactor-unit classification. Each finding is bucketed into one of three
// classes from its kind+detail, which drives both the PR framing and the
// pilot-safe decision: move-only splits are mechanical and safe, cleanup is
// low-risk, behaviour changes warrant scrutiny.
const (
	classMoveOnly = "move-only"
	classBehavior = "behavior"
	classCleanup  = "cleanup"
)

// PlannedPR is one entry in an ordered refactor plan: a single small, reviewable
// PR derived from a Finding, annotated with its dependency order, blast radius,
// classification, ownership, and whether Pilot may ship it unattended. It is the
// architect-side view; ProjectToEpic also projects the same sequence onto the
// executor's EpicPlan so it can feed createSubIssuesViaAdapter unchanged.
type PlannedPR struct {
	// Title is the imperative PR title (carried from the Finding).
	Title string
	// Class is one of move-only | behavior | cleanup.
	Class string
	// Order is the 1-indexed execution position in the sequence.
	Order int
	// DependsOn lists the Orders this PR depends on (lower-blast siblings it
	// must land after). Empty for a leaf.
	DependsOn []int
	// BlastRadius is the number of packages that transitively depend on the
	// files this PR touches; 0 for a leaf or an unmapped change.
	BlastRadius int
	// PilotSafe reports whether Pilot may execute this PR unattended. False
	// means manual review is required (high blast radius or a cross-layer
	// behaviour change).
	PilotSafe bool
	// ManualReason explains, when PilotSafe is false, why the PR needs a human.
	ManualReason string
	// Owner is the best-effort top author of the touched files, or "" when
	// unknown (no git / untracked). Used only for annotation.
	Owner string
	// Files are the relative paths the PR touches (carried from the Finding).
	Files []string
	// Risk is the carried-through Finding risk, used for ordering ties.
	Risk pilotapi.RiskLevel
}

// RefactorPlan is the full ordered output of ProjectToEpic: the per-PR view, the
// executor EpicPlan projection (ready for createSubIssuesViaAdapter), and the
// counts the lens reports.
type RefactorPlan struct {
	// PRs is the ordered PR sequence (Order ascending), capped to
	// [minPlanSubtasks, maxPlanSubtasks].
	PRs []PlannedPR
	// Epic is the executor projection of PRs: a parent task plus ordered
	// PlannedSubtasks with the same Order/DependsOn, so the sequence can later
	// feed the epic sub-issue machinery without re-deriving the plan.
	Epic *executor.EpicPlan
	// Manual is the count of PRs flagged manual-review-required.
	Manual int
}

// ProjectToEpic turns ranked refactor Findings into an ordered plan of small
// PRs and projects it onto the executor's EpicPlan shape. Ordering is driven by
// blast radius derived from graph (low-blast / leaf changes first) so dependent
// packages are refactored only after the leaves they rest on; classification and
// the pilot-safe flag are heuristics over each Finding's kind/detail and its
// blast radius. owners (path -> top author, possibly empty) annotates each PR.
//
// The result is deterministic for identical inputs: ordering ties break by risk
// then title, and the sequence is capped to [minPlanSubtasks, maxPlanSubtasks].
// A graph of nil is tolerated (every change is treated as zero-blast).
func ProjectToEpic(findings []pilotapi.Finding, graph *PackageGraph, owners map[string]string) RefactorPlan {
	prs := make([]PlannedPR, 0, len(findings))
	for _, f := range findings {
		prs = append(prs, newPlannedPR(f, graph, owners))
	}

	sortPlannedPRs(prs)
	prs = capPlannedPRs(prs)
	assignOrderAndDeps(prs)

	manual := 0
	for _, pr := range prs {
		if !pr.PilotSafe {
			manual++
		}
	}

	return RefactorPlan{
		PRs:    prs,
		Epic:   projectEpic(prs),
		Manual: manual,
	}
}

// newPlannedPR builds the un-ordered PlannedPR for a single Finding: it
// classifies the unit, computes its blast radius from the graph, resolves
// pilot-safety, and attaches ownership. Order/DependsOn are filled later by
// assignOrderAndDeps once the whole set is sorted.
func newPlannedPR(f pilotapi.Finding, graph *PackageGraph, owners map[string]string) PlannedPR {
	class := classifyFinding(f)
	blast := blastRadiusFor(f.Files, graph)
	safe, reason := pilotSafety(class, blast, f)

	return PlannedPR{
		Title:        strings.TrimSpace(f.Title),
		Class:        class,
		BlastRadius:  blast,
		PilotSafe:    safe,
		ManualReason: reason,
		Owner:        ownerForFiles(f.Files, owners),
		Files:        f.Files,
		Risk:         normalizeRisk(f.Risk),
	}
}

// classifyFinding buckets a Finding into move-only | behavior | cleanup from its
// kind and detail text. The heuristic is conservative: an unmistakable mechanical
// split reads as move-only; TODO/cleanup chores read as cleanup; everything that
// could change runtime behaviour (cycles, layer fixes, coverage, bugs) reads as
// behavior so it is never silently auto-shipped.
func classifyFinding(f pilotapi.Finding) string {
	hay := strings.ToLower(f.Kind + " " + f.Title + " " + f.WhyItMatters)
	switch {
	case mentionsAny(hay, "split", "decompose", "move-only", "extract file", "under the loc"):
		return classMoveOnly
	case mentionsAny(hay, "todo", "fixme", "unused dependency", "drop ", "remove dead", "stale"):
		return classCleanup
	default:
		return classBehavior
	}
}

// pilotSafety decides whether Pilot may ship a PR unattended and, if not, why.
// A high blast radius makes any change manual (it ripples too far); a behaviour
// change at a release-blocker / high risk also requires review. Move-only and
// cleanup units at a contained blast radius are pilot-safe.
func pilotSafety(class string, blast int, f pilotapi.Finding) (bool, string) {
	if blast >= highBlastRadius {
		return false, fmt.Sprintf("high blast radius (%d dependent packages) — manual review required", blast)
	}
	if class == classBehavior {
		switch f.Risk {
		case pilotapi.RiskReleaseBlocker, pilotapi.RiskHigh:
			return false, "behaviour change at " + string(f.Risk) + " risk — manual review required"
		}
	}
	return true, ""
}

// blastRadiusFor maps a Finding's relative file paths onto graph package nodes
// and returns the size of the union of their blast radii: how many packages
// transitively depend on the touched packages. A nil graph or files that map to
// no known package yield 0 (treated as a safe leaf).
func blastRadiusFor(files []string, graph *PackageGraph) int {
	if graph == nil || len(files) == 0 {
		return 0
	}
	pkgs := packagesForFiles(files, graph)
	affected := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, dep := range graph.BlastRadius(pkg) {
			affected[dep] = true
		}
	}
	return len(affected)
}

// packagesForFiles resolves the set of graph package import paths a list of
// relative file paths belongs to. A file's package directory is matched against
// graph nodes by suffix (the node is a full module import path, the file path is
// module-relative), so "internal/architect/foo.go" matches the node ending in
// "/internal/architect". Unmatched files are dropped.
func packagesForFiles(files []string, graph *PackageGraph) []string {
	nodes := graph.Nodes()
	seen := make(map[string]bool)
	var out []string
	for _, f := range files {
		dir := path.Dir(filePathToSlash(f))
		if dir == "." || dir == "/" || dir == "" {
			continue
		}
		for _, n := range nodes {
			if n == dir || strings.HasSuffix(n, "/"+dir) {
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// filePathToSlash normalises a possibly-backslash path to forward slashes so
// suffix matching against import paths is OS-independent.
func filePathToSlash(p string) string {
	return strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
}

// ownerForFiles returns the owner annotation for a PR: the top author shared by
// the most touched files, or "" when no file has a known owner. Deterministic by
// author-name tiebreak.
func ownerForFiles(files []string, owners map[string]string) string {
	if len(owners) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, f := range files {
		if o := owners[filePathToSlash(f)]; o != "" {
			counts[o]++
		}
		if o := owners[strings.TrimSpace(f)]; o != "" {
			counts[o]++
		}
	}
	best := ""
	bestN := 0
	for o, n := range counts {
		if n > bestN || (n == bestN && o < best) {
			best, bestN = o, n
		}
	}
	return best
}

// mentionsAny reports whether hay contains any of needles.
func mentionsAny(hay string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}
