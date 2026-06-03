package architect

import (
	"context"
	"fmt"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// FailureSource is the small slice of *memory.Store the churn and bug-history
// collectors need: a breakdown of recent failure reasons used as a
// churn/instability proxy. Declaring it locally lets tests inject a fake and
// keeps architect from hard-depending on the full Store surface. It is exported
// so the CLI can name it when wiring a memory store into ScanOptions.
type FailureSource interface {
	GetFailureReasons(query memory.MetricsQuery, limit int) ([]*memory.FailureReason, error)
}

// failureSource is the internal alias used across the package's collectors.
type failureSource = FailureSource

// pitfallSource is the small slice of *memory.KnowledgeStore the pitfall
// collector needs: experiential pitfalls recorded for the project.
type pitfallSource interface {
	QueryByType(memType memory.MemoryType, projectID string) ([]*memory.Memory, error)
}

// churnThreshold is the failure count at or above which a recurring failure
// reason is treated as a churn hotspot worth surfacing.
const churnThreshold = 2

// ChurnCollector surfaces recurring execution failures as churn_hotspot
// Signals, using the memory store's failure-reason breakdown as a proxy for
// unstable, frequently-touched areas. It is best-effort: a nil source or a
// query error yields zero Signals and no error.
type ChurnCollector struct {
	source    failureSource
	query     memory.MetricsQuery
	limit     int
	projectID string
}

// NewChurnCollector builds a ChurnCollector over source, scoped by query
// (time window + projects) and capped at limit reasons. projectID is recorded
// on emitted Signals for traceability. If source is nil the collector is
// inert.
func NewChurnCollector(source failureSource, query memory.MetricsQuery, limit int, projectID string) *ChurnCollector {
	if limit <= 0 {
		limit = 10
	}
	return &ChurnCollector{source: source, query: query, limit: limit, projectID: projectID}
}

// Name implements Collector.
func (c *ChurnCollector) Name() string { return "churn_hotspot" }

// Collect queries recent failure reasons and emits a churn_hotspot Signal for
// each reason whose count meets churnThreshold. A nil source or a query error
// yields zero Signals and a nil error.
func (c *ChurnCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	if c.source == nil {
		return nil, nil
	}
	reasons, err := c.source.GetFailureReasons(c.query, c.limit)
	if err != nil {
		return nil, nil
	}
	var signals []Signal
	for _, r := range reasons {
		if r == nil || r.Count < churnThreshold {
			continue
		}
		signals = append(signals, Signal{
			Kind:   c.Name(),
			File:   "",
			Line:   0,
			Detail: fmt.Sprintf("%d recent failures: %s", r.Count, truncate(r.Reason, 100)),
			Weight: churnWeight(r.Count),
			Risk:   churnRisk(r.Count),
		})
	}
	return signals, nil
}

// churnWeight scales with the failure count, normalised so a count equal to
// churnThreshold scores 1.0.
func churnWeight(count int) float64 {
	if churnThreshold <= 0 {
		return float64(count)
	}
	return float64(count) / float64(churnThreshold)
}

// churnRisk classifies a churn hotspot: high once failures reach 3x the
// threshold, medium otherwise.
func churnRisk(count int) pilotapi.RiskLevel {
	if count >= churnThreshold*3 {
		return pilotapi.RiskHigh
	}
	return pilotapi.RiskMedium
}

// PitfallCollector surfaces recorded pitfalls for the project as known_pitfall
// Signals. It is best-effort: a nil source or a query error yields zero
// Signals and no error.
type PitfallCollector struct {
	source    pitfallSource
	projectID string
}

// NewPitfallCollector builds a PitfallCollector over source, scoped to
// projectID. If source is nil the collector is inert.
func NewPitfallCollector(source pitfallSource, projectID string) *PitfallCollector {
	return &PitfallCollector{source: source, projectID: projectID}
}

// Name implements Collector.
func (c *PitfallCollector) Name() string { return "known_pitfall" }

// Collect queries pitfall memories for the project and emits one known_pitfall
// Signal per recorded pitfall. Higher-confidence pitfalls carry more weight. A
// nil source or a query error yields zero Signals and a nil error.
func (c *PitfallCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	if c.source == nil {
		return nil, nil
	}
	mems, err := c.source.QueryByType(memory.MemoryTypePitfall, c.projectID)
	if err != nil {
		return nil, nil
	}
	var signals []Signal
	for _, m := range mems {
		if m == nil {
			continue
		}
		signals = append(signals, Signal{
			Kind:   c.Name(),
			File:   m.Context,
			Line:   0,
			Detail: truncate(m.Content, 120),
			Weight: m.Confidence,
			Risk:   pilotapi.RiskMedium,
		})
	}
	return signals, nil
}
