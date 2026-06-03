package architect

// RFCLensName is the selector for the RFC-generator lens.
const RFCLensName = "rfc"

// IsRFCLens reports whether name selects the RFC-generator lens, matching the
// same case-insensitive, whitespace-tolerant rules as the --lens flag. The CLI
// uses it to route the rfc lens onto its document render/write path instead of
// the issue-emitting pipeline.
func IsRFCLens(name string) bool {
	return normalizeLensName(name) == RFCLensName
}

// ADRDir is the directory RFC/ADR drafts land in: Navigator's long-lived
// architecture-decision store. The rfc lens writes `<ADRDir>/rfc_<slug>.md`
// there only when a write flag (--create-issues) is set; in dry-run it prints
// the draft and the would-write path instead.
const ADRDir = ".agent/system"

// rfcTaskDescription is the .agent guidance hint for the rfc lens so the overlay
// machinery surfaces architecture-decision SOPs/conventions when the backend
// path drafts the narrative prose.
const rfcTaskDescription = "draft an RFC/ADR for a refactor plus a sequence of tiny, ordered PRs"

// rfcHeading and rfcIntro frame the (backend) PROPOSE prompt for the rfc lens:
// the goal is an architecture-decision document whose narrative sections wrap a
// deterministic tiny-PR sequence, not a flat list of refactor proposals.
const (
	rfcHeading = "RFC / ADR Generator"
	rfcIntro   = "A deterministic scan surfaced the refactor units below. Draft an RFC/ADR that " +
		"frames them as a single architectural decision: a problem statement, the constraints any " +
		"solution must respect, the alternatives weighed, the decision taken, a phased rollout, and " +
		"the risk plus a concrete rollback path. The tiny-PR sequence itself is generated " +
		"deterministically from the same signals — your job is the prose around it."
)

// rfcInstruction is the lens's analyzer slant: it asks the backend for exactly
// the six narrative sections of the RFC, each as one finding whose why_it_matters
// carries the prose, so the EMIT/compose stage can slot them into the document
// template without re-parsing free-form output.
const rfcInstruction = "Produce the RFC's narrative sections. For each of the six sections — " +
	"Problem Statement, Constraints, Alternatives Considered, Decision, Rollout Plan, and " +
	"Risk & Rollback — emit one finding whose TITLE names the section and whose why_it_matters " +
	"is the section prose. Keep the prose tight and decision-oriented; do NOT enumerate the " +
	"per-PR steps (those are generated deterministically). Set kind to \"refactor\"."

// rfcSlant is the shared slant the rfc lens applies to the backend PROPOSE
// stage. Package-level (immutable) so it is not rebuilt per call.
var rfcSlant = &LensSlant{
	TaskDescription:  rfcTaskDescription,
	Heading:          rfcHeading,
	Intro:            rfcIntro,
	ExtraInstruction: rfcInstruction,
}

// rfcCollectors builds the RFC-generator roster: the same deterministic core
// collectors plus the dependency-doctor collector the refactor lens uses, so the
// RFC's tiny-PR sequence is driven by the identical signals and blast-radius
// graph. Reusing refactorCollectors keeps the two lenses in lock-step by
// construction — the RFC is the refactor plan wrapped in an ADR document.
func rfcCollectors(projectPath string, opts ScanOptions) []Collector {
	return refactorCollectors(projectPath, opts)
}

// init registers the RFC-generator lens. Selectable via `pilot architect --lens
// rfc`, it bundles the refactor-relevant deterministic collectors over the
// shared SCAN spine and carries the rfc slant so the backend path drafts the
// narrative prose. The dry-run / offline path composes the same RFC
// deterministically from the synthesized findings via GenerateRFCOffline — no
// LLM, no network — and reports the would-write path without touching disk.
func init() {
	RegisterLens(Lens{
		Name:        RFCLensName,
		Description: "RFC/ADR generator: draft an architecture-decision doc (problem, constraints, alternatives, decision, rollout, risk+rollback) wrapping a deterministic tiny-PR sequence",
		Collectors:  rfcCollectors,
		Slant:       rfcSlant,
	})
}

// GenerateRFCOffline is the offline-deterministic RFC generator: it synthesizes
// findings from the deterministic SCAN signals (no LLM, no network), projects
// them onto the ordered refactor plan via ProjectToEpic, and composes the full
// RFC/ADR document — frontmatter, the six narrative sections, and the tiny-PR
// sequence — entirely from templates. graph drives blast-radius ordering (nil is
// tolerated — every change is a zero-blast leaf); owners annotates each PR's
// suggested reviewer. The result is reproducible: identical signals/graph always
// yield the identical document and slug.
func GenerateRFCOffline(title string, signals []Signal, graph *PackageGraph, owners map[string]string) RFCDoc {
	findings := SynthesizeFindings(signals)
	plan := ProjectToEpic(findings, graph, owners)
	return BuildRFCDoc(title, findings, plan, RFCProse{})
}
