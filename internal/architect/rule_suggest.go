package architect

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// KindRuleSuggestion is the Signal kind the RuleSuggester emits: a DRAFT
// guardrail rule mined deterministically from recorded pitfall/decision
// memories and repeated forbidden-import violations.
//
// A rule_suggestion Signal is ADVISORY ONLY. It is never promoted into the
// live RuleRegistry or defaultLayerRules — promotion is a human editing
// defaultLayerRules after reviewing the draft. The suggester is deterministic
// and LLM-free by construction.
const KindRuleSuggestion = "rule_suggestion"

// ruleSuggestEdgeThreshold is the number of distinct triggers (memory mentions
// plus repeated violations) an unseen From->To edge must accumulate before the
// suggester drafts a rule for it. A single mention is treated as noise; two
// independent triggers indicate a recurring boundary worth proposing.
const ruleSuggestEdgeThreshold = 2

// edgePhrasePattern matches the conservative set of layering phrasings the
// suggester mines from memory content and violation details. It captures two
// package-path-like tokens around a forbidden-edge verb:
//
//	"<from> must not import <to>"
//	"<from> should not import <to>"
//	"<from> must not depend on <to>"
//	"<from> depends on <to>"
//	"<from> imports <to>"
//	"forbidden import <from> -> <to>"
//
// Only slash/dot package-path tokens are accepted as From/To so free prose
// ("auth changes break tests") never produces a spurious edge.
var edgePhrasePattern = regexp.MustCompile(
	`(?i)([\w./-]+)\s+(?:must not import|should not import|must not depend on|depends on|imports)\s+([\w./-]+)`,
)

// forbiddenImportArrowPattern matches the live layer_violation Detail rendering
// ("forbidden import <from> -> <to> (rule ...)") so repeated violations on an
// edge can be aggregated independent of which rule produced them.
var forbiddenImportArrowPattern = regexp.MustCompile(
	`forbidden import\s+([\w./-]+)\s*->\s*([\w./-]+)`,
)

// packagePathToken is the shape an extracted From/To token must satisfy to be
// treated as a package reference: it must look like an import path (contain a
// slash or a dot) rather than a bare English word. This is the deterministic
// guard that keeps the suggester conservative.
var packagePathToken = regexp.MustCompile(`[/.]`)

// ruleEdge is a candidate forbidden import direction From->To accumulated by
// the suggester, with the Signals that triggered it for traceability.
type ruleEdge struct {
	from     string
	to       string
	triggers []string // human-readable origin of each trigger, sorted
}

// RuleSuggester mines DRAFT guardrail rules from pitfall/decision memory
// Signals and repeated forbidden-import violation Signals. It is OFF by default
// and only runs when explicitly enabled, because its output is human-review
// material — never an enforced rule.
//
// Extraction is deterministic and conservative:
//
//   - it parses recurring "X must not import Y" / "X depends on Y" phrasings
//     from known_pitfall/known_decision Signal details;
//   - it aggregates repeated layer_violation Signals on the same From->To edge;
//   - it drops any edge already covered by defaultLayerRules (a rule exists);
//   - it requires ruleSuggestEdgeThreshold independent triggers before drafting;
//   - each emitted Signal's Detail renders a draft LayerRule as TEXT, explicitly
//     marked DRAFT, linked to the triggering memories/violations.
type RuleSuggester struct {
	enabled    bool
	knownRules []LayerRule
}

// NewRuleSuggester builds a suggester. When enabled is false the suggester is
// inert (Suggest returns no Signals). knownRules is the set of already-live
// layer rules used for dedup; pass defaultLayerRules in production.
func NewRuleSuggester(enabled bool, knownRules []LayerRule) *RuleSuggester {
	return &RuleSuggester{enabled: enabled, knownRules: knownRules}
}

// Name returns the Signal kind the suggester produces.
func (s *RuleSuggester) Name() string { return KindRuleSuggestion }

// Suggest scans the given Signals and returns rule_suggestion Signals for
// unseen forbidden-import edges that cleared the trigger threshold. When the
// suggester is disabled it returns nil. Output is deterministic: edges are
// sorted by (From, To) and each suggestion lists its triggers in sorted order.
func (s *RuleSuggester) Suggest(signals []Signal) []Signal {
	if !s.enabled {
		return nil
	}

	edges := map[string]*ruleEdge{}
	for _, sig := range signals {
		from, to, origin, ok := extractEdge(sig)
		if !ok {
			continue
		}
		if s.edgeAlreadyCovered(from, to) {
			continue
		}
		key := from + "\x00" + to
		e, seen := edges[key]
		if !seen {
			e = &ruleEdge{from: from, to: to}
			edges[key] = e
		}
		e.triggers = append(e.triggers, origin)
	}

	out := make([]Signal, 0, len(edges))
	for _, e := range sortedEdges(edges) {
		if len(e.triggers) < ruleSuggestEdgeThreshold {
			continue
		}
		out = append(out, s.draftSignal(e))
	}
	return out
}

// extractEdge pulls a From->To package edge out of a single Signal. It accepts
// only the memory/violation kinds the suggester mines, and only when both
// extracted tokens look like package paths. origin is a short, deterministic
// description of what triggered the edge, for traceability in the draft.
func extractEdge(sig Signal) (from, to, origin string, ok bool) {
	switch sig.Kind {
	case KnownPitfallKind, KnownDecisionKind:
		if m := edgePhrasePattern.FindStringSubmatch(sig.Detail); m != nil {
			f, t := normalizePkg(m[1]), normalizePkg(m[2])
			if isPackagePath(f) && isPackagePath(t) {
				return f, t, fmt.Sprintf("%s: %s", sig.Kind, truncate(strings.TrimSpace(sig.Detail), 80)), true
			}
		}
	case kindLayerViolation:
		if m := forbiddenImportArrowPattern.FindStringSubmatch(sig.Detail); m != nil {
			f, t := normalizePkg(m[1]), normalizePkg(m[2])
			if isPackagePath(f) && isPackagePath(t) {
				return f, t, fmt.Sprintf("%s: %s -> %s", kindLayerViolation, f, t), true
			}
		}
	}
	return "", "", "", false
}

// edgeAlreadyCovered reports whether a rule covering From->To already exists in
// the suggester's known rules. Coverage is prefix-based to mirror how
// CheckLayerRules matches: a rule From/To prefix that the candidate edge falls
// under means the live rule would already catch this edge, so suggesting it
// would be a duplicate.
func (s *RuleSuggester) edgeAlreadyCovered(from, to string) bool {
	for _, r := range s.knownRules {
		if underPrefix(from, r.From) && underPrefix(to, r.To) {
			return true
		}
	}
	return false
}

// draftSignal renders one accumulated edge into a rule_suggestion Signal whose
// Detail is a DRAFT LayerRule the reviewer can copy into defaultLayerRules. The
// draft is text only: it is never parsed back into a live Rule.
func (s *RuleSuggester) draftSignal(e *ruleEdge) Signal {
	name := suggestedRuleName(e.from, e.to)
	reason := fmt.Sprintf(
		"suggested from %d recorded signal(s); review before promoting to a live guardrail",
		len(e.triggers),
	)

	triggers := append([]string(nil), e.triggers...)
	sort.Strings(triggers)
	triggers = dedupStrings(triggers)

	var b strings.Builder
	fmt.Fprintf(&b, "DRAFT guardrail rule (NOT enforced — review and add to defaultLayerRules to promote):\n")
	fmt.Fprintf(&b, "LayerRule{Name: %q, From: %q, To: %q, Reason: %q}\n", name, e.from, e.to, reason)
	fmt.Fprintf(&b, "Triggered by:\n")
	for _, t := range triggers {
		fmt.Fprintf(&b, "  - %s\n", t)
	}

	return Signal{
		Kind:   KindRuleSuggestion,
		File:   e.from,
		Detail: strings.TrimRight(b.String(), "\n"),
		Weight: float64(len(e.triggers)),
		Risk:   pilotapi.RiskLow,
	}
}

// suggestedRuleName derives a stable, readable rule name from an edge, e.g.
// "internal/foo" -> "internal/bar" becomes "foo-must-not-import-bar".
func suggestedRuleName(from, to string) string {
	return fmt.Sprintf("%s-must-not-import-%s", lastPathSegment(from), lastPathSegment(to))
}

// lastPathSegment returns the final slash-delimited segment of a package path,
// used to keep generated rule names short.
func lastPathSegment(pkg string) string {
	if i := strings.LastIndex(pkg, "/"); i >= 0 && i+1 < len(pkg) {
		return pkg[i+1:]
	}
	return pkg
}

// normalizePkg trims surrounding punctuation/whitespace a phrasing match may
// carry (trailing periods, commas) so edge keys are stable.
func normalizePkg(tok string) string {
	return strings.Trim(strings.TrimSpace(tok), ".,;:")
}

// isPackagePath reports whether a token looks like a package import path rather
// than a bare English word: it must be non-empty and contain a slash or dot.
func isPackagePath(tok string) bool {
	return tok != "" && packagePathToken.MatchString(tok)
}

// sortedEdges returns the accumulated edges ordered by (From, To) for
// deterministic emission.
func sortedEdges(edges map[string]*ruleEdge) []*ruleEdge {
	out := make([]*ruleEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from != out[j].from {
			return out[i].from < out[j].from
		}
		return out[i].to < out[j].to
	})
	return out
}
