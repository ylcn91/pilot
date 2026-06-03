package autopilot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// renderGuardrailsComment formats the violations into a Markdown PR comment.
// The comment leads with the invisible guardrailsCommentMarker so a repeat run
// can recognise it, states up front whether the run is report-only or blocking,
// then lists each finding grouped by rule for readability.
//
// Violations are assumed pre-sorted by (Rule, File) (the registry sorts them);
// the renderer sorts defensively anyway so output is deterministic regardless of
// input order.
func renderGuardrailsComment(violations []architect.Violation, mode string) string {
	var b strings.Builder
	b.WriteString(guardrailsCommentMarker)
	b.WriteString("\n## Architectural guardrails\n\n")
	b.WriteString(guardrailsHeadline(len(violations), mode))
	b.WriteString("\n\n")

	for _, group := range groupByRule(violations) {
		fmt.Fprintf(&b, "### `%s`\n\n", group.rule)
		for _, v := range group.items {
			fmt.Fprintf(&b, "- %s **`%s`** — %s\n", riskBadge(v.Risk), v.File, v.Detail)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
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
