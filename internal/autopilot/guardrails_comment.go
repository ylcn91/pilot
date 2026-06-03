package autopilot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// renderGuardrailsComment formats the findings into a Markdown PR comment.
// The comment leads with the invisible guardrailsCommentMarker so a repeat run
// can recognise (and update) it, states up front whether the run is report-only
// or blocking, lists each enforced finding grouped by rule, and finally records
// any rules that were waived via pilot-guardrail-allow exception directives so
// the suppression stays visible rather than silent.
//
// enforced are the violations that count toward the status; excepted are the
// violations a directive waived. Both are assumed pre-sorted by (Rule, File)
// (the registry sorts them); the renderer sorts defensively anyway so output is
// deterministic regardless of input order.
func renderGuardrailsComment(enforced, excepted []architect.Violation, usedExceptions []string, mode string) string {
	var b strings.Builder
	b.WriteString(guardrailsCommentMarker)
	b.WriteString("\n## Architectural guardrails\n\n")
	b.WriteString(guardrailsHeadline(len(enforced), mode))
	b.WriteString("\n\n")

	for _, group := range groupByRule(enforced) {
		fmt.Fprintf(&b, "### `%s`\n\n", group.rule)
		for _, v := range group.items {
			fmt.Fprintf(&b, "- %s **`%s`** — %s\n", riskBadge(v.Risk), v.File, v.Detail)
		}
		b.WriteString("\n")
	}

	writeExceptionsSection(&b, excepted, usedExceptions)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// writeExceptionsSection appends the "Waived" section when at least one rule was
// suppressed by a pilot-guardrail-allow directive. It names the waived rules and
// lists the findings each one suppressed, so an exception is documented in the
// PR rather than vanishing silently.
func writeExceptionsSection(b *strings.Builder, excepted []architect.Violation, usedExceptions []string) {
	if len(excepted) == 0 || len(usedExceptions) == 0 {
		return
	}
	fmt.Fprintf(b, "### Waived via `%s` (%s)\n\n",
		guardrailsAllowDirective, strings.Join(usedExceptions, ", "))
	for _, group := range groupByRule(excepted) {
		fmt.Fprintf(b, "- `%s`:\n", group.rule)
		for _, v := range group.items {
			fmt.Fprintf(b, "  - ~~`%s`~~ — %s\n", v.File, v.Detail)
		}
	}
	b.WriteString("\n")
}

// guardrailsHeadline is the one-line summary under the heading.
func guardrailsHeadline(n int, mode string) string {
	noun := "violation"
	if n != 1 {
		noun = "violations"
	}
	if mode == guardrailsModeBlock {
		return fmt.Sprintf("Found **%d** %s. This run is in **block** mode: "+
			"the `%s` commit status is set to failure.", n, noun, guardrailsStatusContext)
	}
	return fmt.Sprintf("Found **%d** %s. This is **report-only** — the PR is "+
		"not blocked; the `%s` status stays green.", n, noun, guardrailsStatusContext)
}

// riskBadge maps a risk level to a short, plain-text severity tag.
func riskBadge(r pilotapi.RiskLevel) string {
	switch r {
	case pilotapi.RiskHigh:
		return "[high]"
	case pilotapi.RiskMedium:
		return "[medium]"
	case pilotapi.RiskLow:
		return "[low]"
	default:
		return "[info]"
	}
}

// ruleGroup is a set of violations sharing one rule name, used to render the
// comment grouped under per-rule headings.
type ruleGroup struct {
	rule  string
	items []architect.Violation
}

// groupByRule buckets violations by Rule and returns the buckets sorted by rule
// name; within each bucket items are sorted by File. The result is deterministic
// for any input ordering.
func groupByRule(violations []architect.Violation) []ruleGroup {
	byRule := make(map[string][]architect.Violation)
	for _, v := range violations {
		byRule[v.Rule] = append(byRule[v.Rule], v)
	}

	names := make([]string, 0, len(byRule))
	for name := range byRule {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ruleGroup, 0, len(names))
	for _, name := range names {
		items := byRule[name]
		sort.SliceStable(items, func(i, j int) bool { return items[i].File < items[j].File })
		out = append(out, ruleGroup{rule: name, items: items})
	}
	return out
}
