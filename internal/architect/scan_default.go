package architect

import "github.com/ylcn91/pilot/internal/quality"

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
	runner := gateRunnerFromQuality(opts.QualityRunner)

	candidates := []Collector{
		NewLOCCollector(),
		NewTODOCollector(),
		NewLintCollector(runner, "lint"),
	}
	if opts.MinCoverage > 0 {
		candidates = append(candidates, NewCoverageCollector(runner, "coverage", opts.MinCoverage))
	}

	selected := filterCollectors(candidates, opts.Signals)
	return NewScanner(selected...)
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
