package architect

import (
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
)

// ScanOptions tunes which deterministic collectors BuildDefaultScanner wires
// and how they are configured. Zero values fall back to each collector's own
// default: LOC 0 keeps the built-in LOCThreshold, MinCoverage 0 disables the
// coverage flag, and an empty Signals set runs the full collector roster.
type ScanOptions struct {
	// QualityRunner backs the lint and coverage collectors. A nil *quality.Runner
	// leaves both collectors inert (they emit zero Signals without erroring),
	// which is the correct behaviour when no quality config is available. The
	// concrete pointer type (not gateRunner) is taken deliberately: passing a
	// typed-nil *quality.Runner through an interface field would produce a
	// non-nil interface wrapping a nil pointer, defeating the collectors' own
	// nil check. BuildDefaultScanner converts it safely.
	QualityRunner *quality.Runner

	// MinCoverage is the coverage percentage [0,100] below which a package is
	// flagged. Zero disables the coverage collector.
	MinCoverage float64

	// Signals optionally restricts the roster to collectors whose Name is in
	// this set. Empty includes every default collector.
	Signals []string

	// FailureSource backs the bug-history collector of the test-gap lens: the
	// memory store's recurring-failure breakdown. A nil source (the default)
	// leaves that collector inert, so lenses that read it degrade gracefully
	// when no memory store is configured.
	FailureSource failureSource

	// FailureQuery scopes the failure-reason query the bug-history collector
	// runs (time window + projects). The zero value queries all projects across
	// all time.
	FailureQuery memory.MetricsQuery

	// ProjectID tags Signals that originate from memory-backed collectors for
	// traceability. Empty is acceptable.
	ProjectID string

	// KnowledgeSource backs the pitfall/decision collectors that feed the
	// guardrail rule-suggester: a typed lookup over the memory knowledge store.
	// A nil source (the default) leaves those collectors out of the roster, so
	// the suggester stays inert when no knowledge store is configured.
	KnowledgeSource PitfallSource

	// SuggestRules enables the guardrail rule-suggester path: when true (and a
	// KnowledgeSource is present) the pitfall/decision collectors are wired into
	// the roster so SuggestRulesFromSignals can mine DRAFT layer rules from their
	// Signals. It is OFF by default because the suggester's output is advisory,
	// human-review material — never an enforced rule.
	SuggestRules bool
}

// gateRunnerFromQuality adapts a *quality.Runner to the gateRunner slice the
// quality collectors depend on. It returns nil for a nil runner so the
// collectors stay inert rather than panicking.
func gateRunnerFromQuality(r *quality.Runner) gateRunner {
	if r == nil {
		return nil
	}
	return r
}

// BuildDefaultScanner assembles the deterministic SCAN roster the CLI and
// scheduler use: the file-walk collectors (oversized files, TODO/FIXME) plus
// the optional quality-gate collectors (lint, coverage). The roster is filtered
// by opts.Signals when non-empty. The result is always a usable Scanner — an
// empty roster simply produces no Signals.
func BuildDefaultScanner(opts ScanOptions) *Scanner {
	selected := filterCollectors(coreCollectors(opts), opts.Signals)
	return NewScanner(selected...)
}

// coreCollectors builds the deterministic core roster from opts: the file-walk
// collectors (oversized files, TODO/FIXME) plus the optional quality-gate
// collectors (lint, coverage). It is the single source of truth shared by
// BuildDefaultScanner and the registered "core" lens, so the legacy default
// path and the lens path never drift.
func coreCollectors(opts ScanOptions) []Collector {
	runner := gateRunnerFromQuality(opts.QualityRunner)

	candidates := []Collector{
		NewLOCCollector(),
		NewTODOCollector(),
		NewLintCollector(runner, "lint"),
	}
	if opts.MinCoverage > 0 {
		candidates = append(candidates, NewCoverageCollector(runner, "coverage", opts.MinCoverage))
	}
	// The pitfall/decision collectors only join the roster when the rule
	// suggester is explicitly enabled AND a real knowledge source is wired:
	// adding them with a nil source would be dead weight on the default path.
	if opts.SuggestRules && opts.KnowledgeSource != nil {
		candidates = append(candidates,
			NewPitfallCollector(opts.KnowledgeSource, opts.ProjectID),
			NewDecisionCollector(opts.KnowledgeSource, opts.ProjectID),
		)
	}
	return candidates
}

// SuggestRulesFromSignals runs the guardrail rule-suggester over a scan's
// aggregated Signals when opts.SuggestRules is set, returning advisory DRAFT
// rule_suggestion Signals to append. When the flag is off it returns nil. The
// suggester is deterministic and LLM-free; its output is never promoted into
// the live registry — promotion is a human editing defaultLayerRules. Dedup is
// against defaultLayerRules so edges an existing rule already covers are
// dropped.
func SuggestRulesFromSignals(opts ScanOptions, signals []Signal) []Signal {
	return NewRuleSuggester(opts.SuggestRules, defaultLayerRules).Suggest(signals)
}

// filterCollectors keeps only collectors whose Name appears in want. An empty
// want keeps every candidate, preserving order.
func filterCollectors(candidates []Collector, want []string) []Collector {
	if len(want) == 0 {
		return candidates
	}
	allow := make(map[string]bool, len(want))
	for _, w := range want {
		allow[w] = true
	}
	out := make([]Collector, 0, len(candidates))
	for _, c := range candidates {
		if allow[c.Name()] {
			out = append(out, c)
		}
	}
	return out
}
