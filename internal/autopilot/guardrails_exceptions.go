package autopilot

import (
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/architect"
)

// guardrailsAllowDirective is the prefix a PR body or comment line uses to opt a
// single rule out of guardrails enforcement for that PR. The text after the
// prefix is the rule's Name (architect.Rule.Name, e.g. "loc-400"). Matching is
// case-insensitive on the directive itself; the rule name is matched exactly
// (rule names are lowercase, stable identifiers).
//
// Example (in the PR body or any PR comment):
//
//	pilot-guardrail-allow: loc-400
//
// An allowed rule's violations are dropped from the blocking decision and the
// status count, and are listed separately in the comment as acknowledged
// exceptions so the suppression stays visible rather than silent.
const guardrailsAllowDirective = "pilot-guardrail-allow:"

// parseAllowedRules scans free-form text (a PR body or a comment) for
// pilot-guardrail-allow directives and returns the set of rule names they
// authorise. It is forgiving: the directive may appear anywhere on a line, with
// arbitrary surrounding whitespace, and several names may be listed comma- or
// space-separated after one directive. A directive with no rule name yields
// nothing. Returns nil when there are no directives.
func parseAllowedRules(text string) map[string]bool {
	if text == "" {
		return nil
	}
	var allowed map[string]bool
	for _, line := range strings.Split(text, "\n") {
		idx := indexFoldDirective(line)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(guardrailsAllowDirective):]
		for _, name := range splitRuleNames(rest) {
			if allowed == nil {
				allowed = make(map[string]bool)
			}
			allowed[name] = true
		}
	}
	return allowed
}

// indexFoldDirective returns the byte index of the first case-insensitive match
// of the allow directive within line, or -1. strings.Index is case-sensitive, so
// we lower-case a copy purely to locate the prefix; the returned index is valid
// against the original line because ToLower preserves byte length for ASCII (the
// directive is pure ASCII).
func indexFoldDirective(line string) int {
	return strings.Index(strings.ToLower(line), guardrailsAllowDirective)
}

// splitRuleNames extracts rule names from the text following a directive,
// stopping at a comment terminator (--> or #) so a directive embedded in an HTML
// comment or trailing remark does not swallow the rest of the line. Names are
// separated by commas and/or whitespace. Surrounding backticks (Markdown code
// spans) are trimmed.
func splitRuleNames(rest string) []string {
	// Cut at the first comment/markup terminator so "<!-- pilot-guardrail-allow:
	// loc-400 -->" does not capture "-->" as part of the name.
	if i := strings.Index(rest, "-->"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "#"); i >= 0 {
		rest = rest[:i]
	}
	fields := strings.FieldsFunc(rest, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		name := strings.Trim(f, "`\"' ")
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// partitionViolations splits violations into enforced (rule not allowed) and
// excepted (rule explicitly allowed via a directive). Only rules that actually
// appear in allowed AND have at least one violation contribute to usedExceptions
// — listing an allow directive for a rule that did not fire is a no-op, not an
// exception worth reporting. usedExceptions is the sorted, de-duplicated set of
// rule names that suppressed at least one real violation.
func partitionViolations(violations []architect.Violation, allowed map[string]bool) (enforced, excepted []architect.Violation, usedExceptions []string) {
	if len(allowed) == 0 {
		return violations, nil, nil
	}
	used := make(map[string]bool)
	for _, v := range violations {
		if allowed[v.Rule] {
			excepted = append(excepted, v)
			used[v.Rule] = true
			continue
		}
		enforced = append(enforced, v)
	}
	for name := range used {
		usedExceptions = append(usedExceptions, name)
	}
	sort.Strings(usedExceptions)
	return enforced, excepted, usedExceptions
}
