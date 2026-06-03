package architect

import (
	"context"
	"fmt"
	"strings"

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

// KnownPitfallKind is the Signal kind the PitfallCollector emits: a recorded
// pitfall memory replayed as a deterministic Signal so the guardrail
// rule-suggester can mine recurring "X must not import Y" knowledge.
const KnownPitfallKind = "known_pitfall"

// KnownDecisionKind is the Signal kind the DecisionCollector emits: a recorded
// architectural decision replayed as a deterministic Signal, mined alongside
// pitfalls for guardrail rule suggestions.
const KnownDecisionKind = "known_decision"

// memoryPitfallSnippet caps how much of a memory's content is carried in a
// Signal Detail so a verbose memory body does not bloat downstream findings.
const memoryPitfallSnippet = 200

// pitfallSource is the small slice of *memory.KnowledgeStore the memory-backed
// collectors need: a typed lookup of recorded memories. Declaring it locally
// lets tests inject a fake and keeps architect from hard-depending on the full
// store surface. It is exported so the CLI can name it when wiring a knowledge
// store into ScanOptions.
type PitfallSource interface {
	QueryByType(memType memory.MemoryType, projectID string) ([]*memory.Memory, error)
}

// pitfallSource is the internal alias used across the package's memory-backed
// collectors.
type pitfallSource = PitfallSource

// PitfallCollector replays recorded pitfall memories as known_pitfall Signals.
// The Signal Detail carries the memory's content so a downstream consumer (the
// guardrail rule-suggester) can mine recurring architectural pitfalls without
// re-querying the store. It is best-effort: a nil source or a query error
// yields zero Signals and no error.
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
func (c *PitfallCollector) Name() string { return KnownPitfallKind }

// Collect queries recorded pitfall memories and emits one known_pitfall Signal
// per memory. A nil source or a query error yields zero Signals and a nil
// error.
func (c *PitfallCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	return collectMemorySignals(c.source, memory.MemoryTypePitfall, c.projectID, KnownPitfallKind)
}

// DecisionCollector replays recorded architectural-decision memories as
// known_decision Signals, mirroring PitfallCollector. It is best-effort: a nil
// source or a query error yields zero Signals and no error.
type DecisionCollector struct {
	source    pitfallSource
	projectID string
}

// NewDecisionCollector builds a DecisionCollector over source, scoped to
// projectID. If source is nil the collector is inert.
func NewDecisionCollector(source pitfallSource, projectID string) *DecisionCollector {
	return &DecisionCollector{source: source, projectID: projectID}
}

// Name implements Collector.
func (c *DecisionCollector) Name() string { return KnownDecisionKind }

// Collect queries recorded decision memories and emits one known_decision
// Signal per memory. A nil source or a query error yields zero Signals and a
// nil error.
func (c *DecisionCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	return collectMemorySignals(c.source, memory.MemoryTypeDecision, c.projectID, KnownDecisionKind)
}

// collectMemorySignals is the shared body of the pitfall/decision collectors:
// it queries the source for memType memories and renders each into a Signal of
// the given kind. A nil source or query error yields zero Signals and a nil
// error, keeping the collectors fail-open. Empty-content memories are skipped.
func collectMemorySignals(source pitfallSource, memType memory.MemoryType, projectID, kind string) ([]Signal, error) {
	if source == nil {
		return nil, nil
	}
	memories, err := source.QueryByType(memType, projectID)
	if err != nil {
		return nil, nil
	}
	var signals []Signal
	for _, m := range memories {
		if m == nil {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		signals = append(signals, Signal{
			Kind:   kind,
			File:   strings.TrimSpace(m.Context),
			Detail: truncate(content, memoryPitfallSnippet),
			Weight: m.Confidence,
			Risk:   pilotapi.RiskMedium,
		})
	}
	return signals, nil
}
