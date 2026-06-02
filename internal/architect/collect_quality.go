package architect

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
	"github.com/ylcn91/pilot/internal/quality"
)

// gateRunner is the small slice of *quality.Runner the quality collectors
// depend on. Defining it locally keeps the collectors testable with a mock
// and avoids coupling to the Runner's full surface.
type gateRunner interface {
	RunGate(ctx context.Context, gateName string) (*quality.Result, error)
}

// LintCollector runs a lint quality gate and turns each reported finding into
// a Signal. It is best-effort: if the lint tool is absent or the gate cannot
// be run, it emits zero Signals and does not error, so a project without
// golangci-lint installed still scans cleanly.
type LintCollector struct {
	runner   gateRunner
	gateName string
}

// NewLintCollector builds a LintCollector over the given Runner, running the
// gate named gateName (e.g. "lint"). If runner is nil the collector is inert
// (zero Signals, no error).
func NewLintCollector(runner gateRunner, gateName string) *LintCollector {
	return &LintCollector{runner: runner, gateName: gateName}
}

// Name implements Collector.
func (c *LintCollector) Name() string { return "lint" }

// Collect runs the lint gate and parses its output into per-finding Signals.
// A missing runner, an unconfigured gate, or a run error all yield zero
// Signals and a nil error: lint findings are advisory, never fatal to a scan.
func (c *LintCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	if c.runner == nil {
		return nil, nil
	}
	res, err := c.runner.RunGate(ctx, c.gateName)
	if err != nil || res == nil {
		// golangci-lint absent / gate not configured: emit nothing, don't error.
		return nil, nil
	}
	if res.Passed() {
		return nil, nil
	}
	return parseLintOutput(res.Output), nil
}

// lintLineRe matches the conventional golangci-lint / go vet location prefix
// "path/to/file.go:LINE:COL:" or "path/to/file.go:LINE:" at the start of a
// line, capturing the file and line number.
var lintLineRe = regexp.MustCompile(`^([^\s:][^:]*\.go):(\d+):(?:\d+:)?\s*(.*)$`)

// parseLintOutput extracts a Signal for each diagnostic line in raw lint
// output. Lines that do not look like a file:line diagnostic are ignored, so
// banner/summary noise does not produce spurious Signals.
func parseLintOutput(raw string) []Signal {
	var signals []Signal
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		m := lintLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNum := atoiSafe(m[2])
		signals = append(signals, Signal{
			Kind:   "lint",
			File:   m[1],
			Line:   lineNum,
			Detail: truncate(strings.TrimSpace(m[3]), 120),
			Weight: 0.5,
			Risk:   pilotapi.RiskLow,
		})
	}
	return signals
}

// CoverageCollector runs a coverage quality gate and emits a single
// low_coverage Signal when the measured coverage falls below the configured
// threshold. Like LintCollector it is best-effort: a missing runner or a gate
// that cannot run yields zero Signals and no error.
type CoverageCollector struct {
	runner    gateRunner
	gateName  string
	threshold float64
}

// NewCoverageCollector builds a CoverageCollector that runs the gate named
// gateName and flags coverage strictly below threshold (a percentage in
// [0,100]). If runner is nil the collector is inert.
func NewCoverageCollector(runner gateRunner, gateName string, threshold float64) *CoverageCollector {
	return &CoverageCollector{runner: runner, gateName: gateName, threshold: threshold}
}

// Name implements Collector.
func (c *CoverageCollector) Name() string { return "coverage" }

// Collect runs the coverage gate and, when the result's coverage is below the
// threshold, emits one low_coverage Signal. A missing runner or run error
// yields zero Signals and a nil error.
func (c *CoverageCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	if c.runner == nil {
		return nil, nil
	}
	res, err := c.runner.RunGate(ctx, c.gateName)
	if err != nil || res == nil {
		return nil, nil
	}
	if res.Coverage >= c.threshold {
		return nil, nil
	}
	return []Signal{{
		Kind:   "low_coverage",
		File:   "",
		Line:   0,
		Detail: fmt.Sprintf("coverage %.1f%% below threshold %.1f%%", res.Coverage, c.threshold),
		Weight: coverageWeight(res.Coverage, c.threshold),
		Risk:   coverageRisk(res.Coverage, c.threshold),
	}}, nil
}

// coverageWeight scales with the size of the coverage shortfall, normalised
// by the threshold so a shortfall equal to the threshold scores 1.0.
func coverageWeight(coverage, threshold float64) float64 {
	if threshold <= 0 {
		return 0
	}
	gap := threshold - coverage
	if gap < 0 {
		gap = 0
	}
	return gap / threshold
}

// coverageRisk classifies a coverage shortfall: high when coverage is at or
// below half the threshold, medium otherwise.
func coverageRisk(coverage, threshold float64) pilotapi.RiskLevel {
	if coverage <= threshold/2 {
		return pilotapi.RiskHigh
	}
	return pilotapi.RiskMedium
}

// atoiSafe parses a base-10 integer, returning 0 on any parse error.
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
