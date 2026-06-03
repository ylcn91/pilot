package architect

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// LOCThreshold is the line count at or above which a Go file is flagged.
// It matches the project-wide "every Go file < 400 LOC" rule, so any file at
// or over the limit is a refactor candidate.
const LOCThreshold = 400

// locHighThreshold is the line count at or above which an oversized file is
// considered high risk rather than medium: a file this large is hard to
// review and split safely.
const locHighThreshold = 600

// LOCCollector flags Go source files whose line count meets or exceeds
// LOCThreshold. It is uncapped: every oversized file produces a Signal.
type LOCCollector struct{}

// NewLOCCollector returns a LOCCollector.
func NewLOCCollector() *LOCCollector { return &LOCCollector{} }

// Name implements Collector.
func (c *LOCCollector) Name() string { return "loc_over_400" }

// Collect walks projectPath and emits one Signal per *.go file with at least
// LOCThreshold lines. Weight scales with how far the file overshoots the
// threshold; Risk is medium up to locHighThreshold and high beyond it.
func (c *LOCCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	var signals []Signal

	err := walkGoFiles(projectPath, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		lines, err := countLines(path)
		if err != nil {
			// Unreadable file: skip it, don't fail the whole collector.
			return nil
		}
		if lines < LOCThreshold {
			return nil
		}
		signals = append(signals, Signal{
			Kind:   c.Name(),
			File:   relOrAbs(projectPath, path),
			Line:   0,
			Detail: fmt.Sprintf("file is %d lines (limit %d)", lines, LOCThreshold),
			Weight: locWeight(lines),
			Risk:   locRisk(lines),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return signals, nil
}

// countLines returns the number of newline-delimited lines in the file at
// path. A trailing line without a final newline is counted. An empty file
// has zero lines.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

// locWeight maps a line count at or above the threshold to a non-negative
// weight that grows with the overage, normalised so a file exactly one
// threshold over the limit scores 1.0.
func locWeight(lines int) float64 {
	overage := lines - LOCThreshold
	if overage < 0 {
		overage = 0
	}
	return float64(overage) / float64(LOCThreshold)
}

// locRisk classifies an oversized file: medium until locHighThreshold, high
// at or beyond it.
func locRisk(lines int) pilotapi.RiskLevel {
	if lines >= locHighThreshold {
		return pilotapi.RiskHigh
	}
	return pilotapi.RiskMedium
}
