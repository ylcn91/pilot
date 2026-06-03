package architect

import (
	"fmt"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// defaultRFCTitle frames a draft generated from an empty/blank title so the
// document and its slug are still well-formed.
const defaultRFCTitle = "Refactor RFC"

// offlineProblem templates the problem statement from the synthesized findings:
// it enumerates the concrete issues the deterministic scan surfaced. An empty
// finding set yields an honest "scan surfaced nothing" statement rather than an
// empty section.
func offlineProblem(findings []pilotapi.Finding) string {
	titles := findingTitles(findings)
	if len(titles) == 0 {
		return "A deterministic scan of this project surfaced no actionable refactor " +
			"signals at the current thresholds. This RFC is a placeholder; there is " +
			"no problem to address until the scan reports findings."
	}
	var b strings.Builder
	b.WriteString("A deterministic scan of this project surfaced the following structural issues, ")
	b.WriteString("each of which raises the cost of safe change:\n\n")
	for _, t := range titles {
		fmt.Fprintf(&b, "- %s\n", t)
	}
	b.WriteString("\nLeft unaddressed, these compound: oversized units resist review, ")
	b.WriteString("layer drift inverts the dependency direction, and untriaged debt erodes ")
	b.WriteString("trust in the codebase. This RFC proposes resolving them as a single, ")
	b.WriteString("ordered sequence of small, independently-mergeable PRs.")
	return b.String()
}

// offlineConstraints templates the constraints section from the plan's sizing
// and safety envelope: the PR-count bounds, the blast-radius ordering rule, and
// the pilot-safe vs manual split. These are the invariants any execution of the
// plan must respect.
func offlineConstraints(findings []pilotapi.Finding, plan RefactorPlan) string {
	var b strings.Builder
	b.WriteString("The refactor must respect these constraints:\n\n")
	fmt.Fprintf(&b, "- **Small, ordered PRs.** The sequence is capped to the [%d, %d] PR envelope the "+
		"epic machinery is built for; each PR is one reviewable step.\n", minPlanSubtasks, maxPlanSubtasks)
	b.WriteString("- **Behaviour-preserving by default.** Move-only and cleanup PRs change no runtime " +
		"behaviour; `go build ./...` and `go test ./...` must pass before and after each step.\n")
	b.WriteString("- **Blast-radius ordering.** Low-blast / leaf changes land first; a PR touching a " +
		"package many others depend on lands only after its leaves.\n")
	fmt.Fprintf(&b, "- **Human gate on high blast.** %d of the %d PR(s) are flagged manual-review-required "+
		"and must not be shipped unattended.\n", plan.Manual, len(plan.PRs))
	if risk := highestFindingRisk(findings); riskRank(risk) >= riskRank(pilotapi.RiskHigh) {
		fmt.Fprintf(&b, "- **Elevated risk.** The highest finding risk is %s, so the rollout assumes "+
			"extra review and a tested rollback path.\n", risk)
	}
	return strings.TrimRight(b.String(), "\n")
}

// offlineAlternatives templates the alternatives section. The trade-off between
// a big-bang refactor, doing nothing, and the proposed incremental sequence is
// invariant across projects, so the offline text is a fixed, deterministic
// comparison rather than something derived from the signals.
func offlineAlternatives() string {
	return "Three approaches were considered:\n\n" +
		"1. **Big-bang refactor (rejected).** Fixing everything in one large PR minimises " +
		"intermediate states but is unreviewable, blocks the team behind a long-lived branch, " +
		"and makes rollback all-or-nothing.\n" +
		"2. **Do nothing (rejected).** Deferring lets the debt compound; each new change pays " +
		"the oversized-file / layer-drift tax and the blast radius only grows.\n" +
		"3. **Incremental small-PR sequence (chosen).** Splitting the work into ordered, " +
		"independently-mergeable PRs keeps every step reviewable, lets low-risk leaves ship " +
		"unattended, and gates only the high-blast changes behind human review."
}

// offlineDecision templates the decision section from the plan: it states the
// chosen approach and quantifies the sequence (PR count, pilot-safe vs manual).
func offlineDecision(plan RefactorPlan) string {
	if len(plan.PRs) == 0 {
		return "No refactor units were surfaced, so there is nothing to decide yet. " +
			"Re-run the scan once the codebase accrues actionable signals."
	}
	safe := len(plan.PRs) - plan.Manual
	var b strings.Builder
	fmt.Fprintf(&b, "Execute the refactor as an ordered sequence of %d small PR(s), blast-radius "+
		"ordered so leaves land first. ", len(plan.PRs))
	fmt.Fprintf(&b, "Of these, %d are pilot-safe (Pilot may ship them unattended) and %d require "+
		"manual review. ", safe, plan.Manual)
	b.WriteString("Each PR maps one-to-one onto a linked, ordered sub-issue via the epic machinery, " +
		"preserving the dependency edges so the executor runs them in a safe order.")
	return b.String()
}

// offlineRollout templates the rollout plan from the ordered sequence: it walks
// the tiers (leaf-first), states the per-PR verification gate, and how the
// pilot-safe PRs are batched ahead of the manual ones.
func offlineRollout(plan RefactorPlan) string {
	if len(plan.PRs) == 0 {
		return "There is no sequence to roll out. This section becomes actionable once the scan " +
			"produces a tiny-PR sequence."
	}
	var b strings.Builder
	b.WriteString("Roll the sequence out in dependency order:\n\n")
	b.WriteString("1. **Land the leaves first.** Ship the zero-/low-blast PRs at the head of the " +
		"sequence; they unblock the higher-blast PRs that depend on them.\n")
	b.WriteString("2. **Verify each step.** Run `go build ./...` and `go test ./...` after every PR " +
		"so a regression is caught at the smallest possible blast radius.\n")
	b.WriteString("3. **Batch the pilot-safe PRs.** The PRs flagged pilot-safe can be created as linked " +
		"sub-issues and executed unattended via the epic machinery.\n")
	if plan.Manual > 0 {
		fmt.Fprintf(&b, "4. **Gate the %d manual PR(s).** Hold the manual-review-required PRs for a "+
			"human; they carry the cross-layer or high-blast changes.\n", plan.Manual)
	}
	return strings.TrimRight(b.String(), "\n")
}

// offlineRisk templates the risk & rollback section: it names the residual risk
// of the change and the concrete rollback path (revert the offending PR, which
// is safe precisely because the sequence is small and ordered).
func offlineRisk(plan RefactorPlan) string {
	var b strings.Builder
	b.WriteString("**Risk.** ")
	if plan.Manual > 0 {
		fmt.Fprintf(&b, "%d PR(s) carry cross-layer or high-blast changes whose ripple is hard to "+
			"fully predict; these are the primary risk and are gated behind manual review. ", plan.Manual)
	} else {
		b.WriteString("Every PR is contained (move-only, cleanup, or low-blast), so the residual risk " +
			"is low. ")
	}
	b.WriteString("The chief failure mode is a behaviour change slipping into a PR labelled move-only.\n\n")
	b.WriteString("**Rollback.** Because the sequence is small and ordered, rollback is per-PR: revert " +
		"the offending PR (and any later PR that depends on it) to return to a green state. No " +
		"long-lived branch or coordinated multi-PR revert is required, which is the core reason the " +
		"incremental approach was chosen over a big-bang refactor.")
	return b.String()
}
