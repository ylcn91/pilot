package architect

// RefactorLensName is the selector for the refactor-planner lens.
const RefactorLensName = "refactor"

// refactorTaskDescription is the .agent guidance hint for the refactor lens so
// the overlay machinery surfaces refactor SOPs/conventions when the backend path
// is used.
const refactorTaskDescription = "plan a refactor as an ordered sequence of small, independently-mergeable PRs"

// refactorHeading and refactorIntro frame the (backend) PROPOSE prompt for the
// refactor lens: the goal is a dependency-ordered PR sequence, not a flat list.
const (
	refactorHeading = "Refactor Planner"
	refactorIntro   = "A deterministic scan surfaced the refactor units below. Group them into the " +
		"smallest set of independently-mergeable PRs and ORDER them so low-blast-radius / leaf " +
		"changes land first and dependent packages are refactored only after the leaves they rest on."
)

// refactorInstruction is the lens's analyzer slant: it demands an ORDERED,
// dependency-aware PR sequence with per-PR classification and a pilot-safe vs
// manual-review verdict, mirroring the offline ProjectToEpic projection so the
// backend and dry-run paths produce the same shape of plan.
const refactorInstruction = "Produce an ORDERED sequence of small PRs (5–20). For each PR:\n" +
	"  1. CLASSIFY the unit as move-only (mechanical file split, no behaviour change), " +
	"behavior (changes runtime behaviour), or cleanup (TODO/dead-code/unused-dep chore).\n" +
	"  2. ORDER by blast radius: leaf / low-blast-radius changes first; a PR that touches a " +
	"package many others depend on lands only after its leaves.\n" +
	"  3. Record DEPENDENCIES on earlier PRs in the sequence.\n" +
	"  4. FLAG each PR pilot-safe (Pilot may ship it unattended) or manual-review-required " +
	"(cross-layer or high-blast-radius behaviour change).\n" +
	"Set kind to \"refactor\". Keep suggested_pr_pieces to one small, reviewable step per PR."

// refactorSlant is the shared slant the refactor lens applies to the backend
// PROPOSE stage. Package-level (immutable) so it is not rebuilt per call.
var refactorSlant = &LensSlant{
	TaskDescription:  refactorTaskDescription,
	Heading:          refactorHeading,
	Intro:            refactorIntro,
	ExtraInstruction: refactorInstruction,
}

// refactorCollectors builds the refactor-planner roster: the deterministic core
// collectors (oversized files, TODO/FIXME, lint, optional coverage) plus the
// dependency-doctor collector (import cycles / layer drift / heavy/unused deps).
// Together these surface exactly the units a refactor splits into — the file
// decompositions, the cleanup chores, and the structural fixes whose blast
// radius drives the ordering. Every collector degrades gracefully on its own, so
// the lens is safe to run against any project state.
func refactorCollectors(_ string, opts ScanOptions) []Collector {
	collectors := coreCollectors(opts)
	collectors = append(collectors, NewDepsCollector())
	return collectors
}

// init registers the refactor-planner lens. Selectable via `pilot architect
// --lens refactor`, it bundles the refactor-relevant deterministic collectors
// over the shared SCAN spine and carries the refactor slant so the backend path
// returns an ordered PR sequence. The dry-run / offline path synthesizes the
// same findings deterministically and projects them onto the executor EpicPlan
// via PlanRefactorOffline — no LLM, no network.
func init() {
	RegisterLens(Lens{
		Name:        RefactorLensName,
		Description: "refactor planner: turn a refactor target into an ordered 5–20 small-PR sequence (blast-radius ordered, pilot-safe flagged) via the epic machinery",
		Collectors:  refactorCollectors,
		Slant:       refactorSlant,
	})
}

// PlanRefactorOffline is the offline-deterministic refactor planner: it
// synthesizes findings from the deterministic SCAN signals (no LLM, no network),
// then projects them onto an ordered RefactorPlan via ProjectToEpic. graph
// drives blast-radius ordering (nil is tolerated — every change is a zero-blast
// leaf); owners (path -> top author, possibly empty) annotates each PR with a
// suggested reviewer. The result is reproducible: identical signals/graph always
// yield the identical ordered plan and EpicPlan projection.
func PlanRefactorOffline(signals []Signal, graph *PackageGraph, owners map[string]string) RefactorPlan {
	findings := SynthesizeFindings(signals)
	return ProjectToEpic(findings, graph, owners)
}
