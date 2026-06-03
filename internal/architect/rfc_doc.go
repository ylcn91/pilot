package architect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// RFC section headings. They are the canonical ADR/RFC sections the document
// always carries so a reviewer (and the tests) can rely on a fixed structure
// regardless of which signals fed the draft.
const (
	rfcSecProblem      = "## Problem Statement"
	rfcSecConstraints  = "## Constraints"
	rfcSecAlternatives = "## Alternatives Considered"
	rfcSecDecision     = "## Decision"
	rfcSecRollout      = "## Rollout Plan"
	rfcSecRisk         = "## Risk & Rollback"
	rfcSecSequence     = "## Tiny-PR Sequence"
)

// rfcStatusProposed is the lifecycle status stamped into the frontmatter of a
// freshly generated draft. The document is a proposal until a human moves it to
// accepted/rejected, so the generator never emits any other status.
const rfcStatusProposed = "proposed"

// RFCDoc is the structured, deterministic RFC/ADR draft the rfc lens composes
// from the scan signals and the W1 refactor plan. It separates the rendered
// document (Body) from the metadata the lens reports and the on-disk slug, so a
// caller can both print the prose and decide where it would be written without
// re-deriving anything.
type RFCDoc struct {
	// Title is the human RFC title (carried into the frontmatter and the H1).
	Title string
	// Slug is the deterministic, path-safe slug WriteADR uses for the filename.
	Slug string
	// Risk is the highest finding risk that fed the draft, surfaced in the
	// frontmatter so a reader sees the blast level at a glance.
	Risk pilotapi.RiskLevel
	// Body is the fully rendered RFC markdown (frontmatter + all sections).
	Body string
}

// BuildRFCDoc composes a deterministic RFC/ADR draft from the synthesized
// findings and their projected refactor plan. It is the OFFLINE fallback the rfc
// lens uses in dry-run: every section is templated from the inputs, so identical
// findings/plan always yield a byte-identical document — no LLM, no network.
//
// prose, when non-empty, supplies backend-generated text for the narrative
// sections (problem/constraints/alternatives/decision/rollout/risk); any section
// the backend left blank falls back to the deterministic template, so a partial
// backend response still yields a complete document. The tiny-PR sequence is
// always rendered deterministically from the plan so it stays in lock-step with
// the executor EpicPlan projection.
func BuildRFCDoc(title string, findings []pilotapi.Finding, plan RefactorPlan, prose RFCProse) RFCDoc {
	title = strings.TrimSpace(title)
	if title == "" {
		title = defaultRFCTitle
	}
	risk := highestFindingRisk(findings)
	slug := SlugifyADR(title)

	var b strings.Builder
	writeRFCFrontmatter(&b, title, slug, risk, plan)
	fmt.Fprintf(&b, "# %s\n\n", title)
	writeRFCSection(&b, rfcSecProblem, prose.Problem, offlineProblem(findings))
	writeRFCSection(&b, rfcSecConstraints, prose.Constraints, offlineConstraints(findings, plan))
	writeRFCSection(&b, rfcSecAlternatives, prose.Alternatives, offlineAlternatives())
	writeRFCSection(&b, rfcSecDecision, prose.Decision, offlineDecision(plan))
	writeRFCSection(&b, rfcSecRollout, prose.Rollout, offlineRollout(plan))
	writeRFCSection(&b, rfcSecRisk, prose.Risk, offlineRisk(plan))
	writeTinyPRSequence(&b, plan)

	return RFCDoc{Title: title, Slug: slug, Risk: risk, Body: strings.TrimRight(b.String(), "\n") + "\n"}
}

// RFCProse carries optional backend-generated narrative for each RFC section.
// A blank field falls back to the deterministic offline template, so the same
// renderer serves both the configured-backend path and the dry-run fallback.
type RFCProse struct {
	Problem      string
	Constraints  string
	Alternatives string
	Decision     string
	Rollout      string
	Risk         string
}

// ProseFromFindings maps backend-generated findings onto the RFCProse sections
// by matching each finding's Title (case-insensitively) to a known section name
// and taking its WhyItMatters as that section's prose. It is the bridge from the
// rfc slant's "one finding per section" contract back into the document
// renderer. Findings whose title names no section are ignored, so a noisy
// backend response cannot corrupt the document; any section the backend omitted
// stays blank and falls back to the offline template in BuildRFCDoc.
func ProseFromFindings(findings []pilotapi.Finding) RFCProse {
	var p RFCProse
	for _, f := range findings {
		body := strings.TrimSpace(f.WhyItMatters)
		if body == "" {
			continue
		}
		switch {
		case sectionTitleMatches(f.Title, "problem"):
			p.Problem = body
		case sectionTitleMatches(f.Title, "constraint"):
			p.Constraints = body
		case sectionTitleMatches(f.Title, "alternative"):
			p.Alternatives = body
		case sectionTitleMatches(f.Title, "decision"):
			p.Decision = body
		case sectionTitleMatches(f.Title, "rollout"):
			p.Rollout = body
		case sectionTitleMatches(f.Title, "risk"), sectionTitleMatches(f.Title, "rollback"):
			p.Risk = body
		}
	}
	return p
}

// sectionTitleMatches reports whether a finding title names a section by
// containing its keyword, case-insensitively.
func sectionTitleMatches(title, keyword string) bool {
	return strings.Contains(strings.ToLower(title), keyword)
}

// writeRFCFrontmatter emits the YAML frontmatter block: title, slug, status,
// risk, and the PR/manual counts the plan produced. Deterministic field order
// keeps the output reproducible.
func writeRFCFrontmatter(b *strings.Builder, title, slug string, risk pilotapi.RiskLevel, plan RefactorPlan) {
	b.WriteString("---\n")
	fmt.Fprintf(b, "title: %q\n", title)
	fmt.Fprintf(b, "slug: %s\n", slug)
	fmt.Fprintf(b, "status: %s\n", rfcStatusProposed)
	fmt.Fprintf(b, "risk: %s\n", riskOrMedium(risk))
	fmt.Fprintf(b, "pr_count: %d\n", len(plan.PRs))
	fmt.Fprintf(b, "manual_review: %d\n", plan.Manual)
	b.WriteString("---\n\n")
}

// writeRFCSection writes one heading followed by prose when the backend supplied
// it, else the deterministic offline body. The body is always trimmed and
// terminated with a blank line so sections render uniformly.
func writeRFCSection(b *strings.Builder, heading, prose, offline string) {
	body := strings.TrimSpace(prose)
	if body == "" {
		body = strings.TrimSpace(offline)
	}
	b.WriteString(heading)
	b.WriteString("\n\n")
	b.WriteString(body)
	b.WriteString("\n\n")
}

// writeTinyPRSequence renders the ordered, dependency-annotated tiny-PR sequence
// from the refactor plan. Each PR becomes a numbered list entry carrying its
// class, blast radius, pilot-safe verdict, dependencies, and owner, so the RFC's
// rollout is self-contained and mirrors the executor EpicPlan one-to-one. An
// empty plan still writes the heading with an explicit "no PRs" note so the
// section is never silently missing.
func writeTinyPRSequence(b *strings.Builder, plan RefactorPlan) {
	b.WriteString(rfcSecSequence)
	b.WriteString("\n\n")
	if len(plan.PRs) == 0 {
		b.WriteString("_No refactor units were surfaced; the scan produced no tiny-PR sequence._\n")
		return
	}
	for _, pr := range plan.PRs {
		fmt.Fprintf(b, "%d. **%s** — class=%s, blast=%d, %s%s%s\n",
			pr.Order, pr.Title, pr.Class, pr.BlastRadius, pilotVerdict(pr), dependsClause(pr), ownerClause(pr))
	}
}

// pilotVerdict renders the pilot-safe / manual-review verdict for one PR entry,
// inlining the manual reason when present so the sequence line is self-contained.
func pilotVerdict(pr PlannedPR) string {
	if pr.PilotSafe {
		return "pilot-safe"
	}
	if pr.ManualReason != "" {
		return "manual-review (" + pr.ManualReason + ")"
	}
	return "manual-review"
}

// dependsClause renders the ", depends on PR n, m" suffix for a PR, or "" when
// it is a leaf with no dependencies.
func dependsClause(pr PlannedPR) string {
	if len(pr.DependsOn) == 0 {
		return ""
	}
	parts := make([]string, len(pr.DependsOn))
	for i, d := range pr.DependsOn {
		parts[i] = fmt.Sprintf("PR %d", d)
	}
	return ", depends on " + strings.Join(parts, ", ")
}

// ownerClause renders the ", reviewer: <owner>" suffix when a top author is
// known, else "".
func ownerClause(pr PlannedPR) string {
	if pr.Owner == "" {
		return ""
	}
	return ", reviewer: " + pr.Owner
}

// highestFindingRisk returns the most severe risk across the findings, used to
// stamp the frontmatter. An empty set yields medium so the frontmatter always
// carries a canonical level.
func highestFindingRisk(findings []pilotapi.Finding) pilotapi.RiskLevel {
	best := pilotapi.RiskLevel("")
	bestRank := -1
	for _, f := range findings {
		r := normalizeRisk(f.Risk)
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

// findingTitles returns the de-duplicated, sorted finding titles, used by the
// offline section templates to enumerate what the RFC addresses deterministically.
func findingTitles(findings []pilotapi.Finding) []string {
	seen := make(map[string]bool, len(findings))
	var out []string
	for _, f := range findings {
		t := strings.TrimSpace(f.Title)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// riskOrMedium returns risk, or medium when it is empty, so frontmatter/prose
// never print a blank risk.
func riskOrMedium(risk pilotapi.RiskLevel) pilotapi.RiskLevel {
	if risk == "" {
		return pilotapi.RiskMedium
	}
	return risk
}
