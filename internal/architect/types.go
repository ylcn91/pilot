// Package architect implements Pilot's Architect family: a pipeline that
// scans a project for refactor signals, proposes changes, and emits them as
// findings. This file defines the SCAN stage's core abstractions: the
// Signal it produces, the Collector interface its sources implement, and the
// Scanner that runs an ordered registry of collectors and aggregates their
// output.
//
// The SCAN stage is deterministic and LLM-free: every Signal comes from a
// real, reproducible source (file walk, lint run, coverage gate, memory
// query). Downstream PROPOSE/EMIT stages turn aggregated Signals into
// pilotapi.Finding proposals.
package architect

import (
	"context"
	"log/slog"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Signal is a single, deterministic observation about the project produced
// by a Collector. Many Signals aggregate into one or more proposals.
//
// Kind names the category of observation (e.g. "loc_over_400", "todo_fixme",
// "lint", "low_coverage", "churn_hotspot", "known_pitfall"). File and Line
// locate it when applicable (Line is 0 when not line-specific). Detail is a
// short human-readable description. Weight is a non-negative relevance score
// the PROPOSE stage uses to rank/cluster Signals; Risk maps the observation
// onto the shared pilotapi risk scale.
type Signal struct {
	Kind   string             `json:"kind"`
	File   string             `json:"file"`
	Line   int                `json:"line"`
	Detail string             `json:"detail"`
	Weight float64            `json:"weight"`
	Risk   pilotapi.RiskLevel `json:"risk"`
}

// Collector is a single source of Signals. Implementations must be
// deterministic given the same project state and must not panic; transient
// or environmental failures (missing tool, unreadable file) should be
// returned as an error or simply produce zero Signals, never a panic.
type Collector interface {
	// Name returns a short, stable identifier for the collector, used in
	// logs and for deterministic ordering in the Scanner registry.
	Name() string

	// Collect inspects the project rooted at projectPath and returns the
	// Signals it found. Returning a non-nil error tells the Scanner this
	// collector failed; the Scanner logs and continues with the rest.
	Collect(ctx context.Context, projectPath string) ([]Signal, error)
}

// Scanner runs an ordered registry of Collectors and aggregates their
// Signals. A single collector returning an error is tolerated: the Scanner
// logs it and continues, so one broken source never aborts the whole scan.
type Scanner struct {
	collectors []Collector
	log        *slog.Logger
}

// NewScanner builds a Scanner over the given collectors, preserving their
// order. The order is significant: Scan invokes collectors in registry order
// and concatenates their Signals in that order, making the aggregate
// deterministic.
func NewScanner(collectors ...Collector) *Scanner {
	return &Scanner{
		collectors: collectors,
		log:        logging.WithComponent("architect.scan"),
	}
}

// Collectors returns the registered collectors in order. The returned slice
// is a copy; mutating it does not affect the Scanner.
func (s *Scanner) Collectors() []Collector {
	out := make([]Collector, len(s.collectors))
	copy(out, s.collectors)
	return out
}

// Scan runs every registered collector against projectPath in order and
// returns the concatenation of their Signals. If a collector returns an
// error, Scan logs it and continues with the remaining collectors; the error
// does not propagate. Scan itself returns a non-nil error only when ctx is
// cancelled before all collectors have run.
//
// The result is never nil: an empty project (or all-empty collectors) yields
// a non-nil, zero-length slice.
func (s *Scanner) Scan(ctx context.Context, projectPath string) ([]Signal, error) {
	signals := make([]Signal, 0)

	for _, c := range s.collectors {
		if err := ctx.Err(); err != nil {
			return signals, err
		}

		found, err := c.Collect(ctx, projectPath)
		if err != nil {
			s.log.Warn("collector failed, continuing",
				slog.String("collector", c.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}
		signals = append(signals, found...)
	}

	return signals, nil
}
