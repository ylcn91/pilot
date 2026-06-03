package architect

import (
	"context"
	"fmt"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// kindBugHotspot is the Signal kind the bug-history collector emits: an area
// that has produced repeated execution failures, marking it as under-tested or
// fragile and therefore a prime target for the test-gap lens.
const kindBugHotspot = "bug_hotspot"

// bugHistoryThreshold is the failure count at or above which a recurring
// failure reason is surfaced as a bug hotspot. A single one-off failure is
// noise; two or more recurrences point at a real, repeatedly-broken area.
const bugHistoryThreshold = 2

// defaultBugHistoryLimit caps how many distinct failure reasons the collector
// pulls from memory when the caller does not specify one.
const defaultBugHistoryLimit = 20

// genericChurnCollector is the shared body behind ChurnCollector and
// BugHistoryCollector: both surface recurring failure reasons from the memory
// store as Signals, differing only in the emitted Kind and how each reason is
// rendered/weighted/risk-classified. It is best-effort by construction — a nil
// source or a query error yields zero Signals and no error.
type genericChurnCollector struct {
	source    failureSource
	query     memory.MetricsQuery
	limit     int
	threshold int
	projectID string

	kind   string
	detail func(r *memory.FailureReason) string
	weight func(count int) float64
	risk   func(count int) pilotapi.RiskLevel
}

// Name implements Collector.
func (c *genericChurnCollector) Name() string { return c.kind }

// Collect queries recent failure reasons and emits one Signal for each reason
// whose recurrence count meets the threshold. A nil source or a query error
// yields zero Signals and a nil error.
func (c *genericChurnCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	if c.source == nil {
		return nil, nil
	}
	reasons, err := c.source.GetFailureReasons(c.query, c.limit)
	if err != nil {
		return nil, nil
	}

	var signals []Signal
	for _, r := range reasons {
		if r == nil || r.Count < c.threshold {
			continue
		}
		signals = append(signals, Signal{
			Kind:   c.kind,
			File:   "",
			Line:   0,
			Detail: c.detail(r),
			Weight: c.weight(r.Count),
			Risk:   c.risk(r.Count),
		})
	}
	return signals, nil
}

// BugHistoryCollector surfaces recurring execution failures as bug_hotspot
// Signals for the test-gap lens. It reads the memory store's failure-reason
// breakdown (a proxy for "what keeps breaking") and weights each reason by how
// often it has recurred, so the PROPOSE stage can prioritise tests for the most
// frequently-broken areas first.
//
// It is best-effort by construction: a nil source or a query error yields zero
// Signals and no error, so a project with no memory store (or an unreachable
// one) still scans cleanly.
type BugHistoryCollector struct {
	genericChurnCollector
}

// NewBugHistoryCollector builds a BugHistoryCollector over source, scoped by
// query (time window + projects) and capped at limit reasons. projectID is
// recorded on emitted Signals for traceability. A nil source makes the
// collector inert; a non-positive limit falls back to the package default.
func NewBugHistoryCollector(source failureSource, query memory.MetricsQuery, limit int, projectID string) *BugHistoryCollector {
	if limit <= 0 {
		limit = defaultBugHistoryLimit
	}
	threshold := bugHistoryThreshold
	return &BugHistoryCollector{genericChurnCollector{
		source:    source,
		query:     query,
		limit:     limit,
		threshold: threshold,
		projectID: projectID,
		kind:      kindBugHotspot,
		detail: func(r *memory.FailureReason) string {
			return fmt.Sprintf("%d recurring failures, likely under-tested: %s", r.Count, truncate(r.Reason, 100))
		},
		weight: func(count int) float64 { return bugHistoryWeight(count, threshold) },
		risk:   func(count int) pilotapi.RiskLevel { return bugHistoryRisk(count, threshold) },
	}}
}

// bugHistoryWeight scales linearly with the failure count, normalised so a
// count equal to the threshold scores 1.0 and more frequent failures rank
// higher in the PROPOSE stage.
func bugHistoryWeight(count, threshold int) float64 {
	if threshold <= 0 {
		return float64(count)
	}
	return float64(count) / float64(threshold)
}

// bugHistoryRisk classifies a bug hotspot: release-blocker once failures reach
// 5x the threshold (a chronically broken area), high at 3x, medium otherwise.
func bugHistoryRisk(count, threshold int) pilotapi.RiskLevel {
	switch {
	case threshold <= 0:
		return pilotapi.RiskMedium
	case count >= threshold*5:
		return pilotapi.RiskReleaseBlocker
	case count >= threshold*3:
		return pilotapi.RiskHigh
	default:
		return pilotapi.RiskMedium
	}
}
