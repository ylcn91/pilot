package architect

import (
	"context"
	"errors"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// mockFailureSource is a programmable failureSource.
type mockFailureSource struct {
	reasons []*memory.FailureReason
	err     error
}

func (m *mockFailureSource) GetFailureReasons(q memory.MetricsQuery, limit int) ([]*memory.FailureReason, error) {
	return m.reasons, m.err
}

func TestChurnCollector_FlagsRecurringFailures(t *testing.T) {
	src := &mockFailureSource{reasons: []*memory.FailureReason{
		{Reason: "build error", Count: 6}, // >= churnThreshold*3 => high
		{Reason: "flaky test", Count: 2},  // == churnThreshold => medium
		{Reason: "one off", Count: 1},     // below churnThreshold, excluded
		nil,                               // tolerated
	}}
	c := NewChurnCollector(src, memory.MetricsQuery{}, 10, "proj")
	signals, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 churn signals, got %d: %+v", len(signals), signals)
	}
	if signals[0].Kind != "churn_hotspot" {
		t.Errorf("Kind = %q, want churn_hotspot", signals[0].Kind)
	}
	// count 6 >= churnThreshold*3 => high
	if signals[0].Risk != pilotapi.RiskHigh {
		t.Errorf("count 6 should be high risk; got %q", signals[0].Risk)
	}
	// count 2 == churnThreshold => medium
	if signals[1].Risk != pilotapi.RiskMedium {
		t.Errorf("count 2 should be medium risk; got %q", signals[1].Risk)
	}
}

func TestChurnCollector_QueryErrorBestEffort(t *testing.T) {
	src := &mockFailureSource{err: errors.New("db down")}
	signals, err := NewChurnCollector(src, memory.MetricsQuery{}, 10, "proj").Collect(context.Background(), "/p")
	if err != nil || len(signals) != 0 {
		t.Fatalf("query error must be best-effort; got (%+v, %v)", signals, err)
	}
}

func TestChurnCollector_NilSourceInert(t *testing.T) {
	signals, err := NewChurnCollector(nil, memory.MetricsQuery{}, 10, "proj").Collect(context.Background(), "/p")
	if err != nil || signals != nil {
		t.Fatalf("nil source must be inert; got (%+v, %v)", signals, err)
	}
}

func TestChurnCollector_DefaultLimit(t *testing.T) {
	c := NewChurnCollector(&mockFailureSource{}, memory.MetricsQuery{}, 0, "proj")
	if c.limit != 10 {
		t.Errorf("non-positive limit should default to 10, got %d", c.limit)
	}
}

// mockPitfallSource is a programmable pitfallSource keyed by memory type.
type mockPitfallSource struct {
	byType map[memory.MemoryType][]*memory.Memory
	err    error
}

func (m *mockPitfallSource) QueryByType(memType memory.MemoryType, projectID string) ([]*memory.Memory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.byType[memType], nil
}

func TestPitfallCollector_EmitsKnownPitfallSignals(t *testing.T) {
	src := &mockPitfallSource{byType: map[memory.MemoryType][]*memory.Memory{
		memory.MemoryTypePitfall: {
			{Content: "internal/gateway must not import internal/executor", Context: "internal/gateway", Confidence: 0.9},
			{Content: "  ", Context: "x"}, // blank content skipped
			nil,                           // tolerated
		},
	}}
	c := NewPitfallCollector(src, "proj")
	signals, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 pitfall signal, got %d: %+v", len(signals), signals)
	}
	if signals[0].Kind != KnownPitfallKind {
		t.Errorf("Kind = %q, want %q", signals[0].Kind, KnownPitfallKind)
	}
	if signals[0].File != "internal/gateway" {
		t.Errorf("File = %q, want internal/gateway (from Context)", signals[0].File)
	}
	if signals[0].Detail != "internal/gateway must not import internal/executor" {
		t.Errorf("Detail = %q, want the memory content", signals[0].Detail)
	}
	if signals[0].Weight != 0.9 {
		t.Errorf("Weight = %v, want the memory confidence 0.9", signals[0].Weight)
	}
}

func TestDecisionCollector_EmitsKnownDecisionSignals(t *testing.T) {
	src := &mockPitfallSource{byType: map[memory.MemoryType][]*memory.Memory{
		memory.MemoryTypeDecision: {
			{Content: "internal/alerts depends on internal/dashboard", Context: "internal/alerts", Confidence: 0.8},
		},
	}}
	c := NewDecisionCollector(src, "proj")
	signals, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 decision signal, got %d: %+v", len(signals), signals)
	}
	if signals[0].Kind != KnownDecisionKind {
		t.Errorf("Kind = %q, want %q", signals[0].Kind, KnownDecisionKind)
	}
	if signals[0].Detail != "internal/alerts depends on internal/dashboard" {
		t.Errorf("Detail = %q, want the memory content", signals[0].Detail)
	}
}

func TestMemoryCollectors_NilSourceInert(t *testing.T) {
	pSig, pErr := NewPitfallCollector(nil, "proj").Collect(context.Background(), "/p")
	if pErr != nil || pSig != nil {
		t.Fatalf("nil pitfall source must be inert; got (%+v, %v)", pSig, pErr)
	}
	dSig, dErr := NewDecisionCollector(nil, "proj").Collect(context.Background(), "/p")
	if dErr != nil || dSig != nil {
		t.Fatalf("nil decision source must be inert; got (%+v, %v)", dSig, dErr)
	}
}

func TestMemoryCollectors_QueryErrorBestEffort(t *testing.T) {
	src := &mockPitfallSource{err: errors.New("db down")}
	signals, err := NewPitfallCollector(src, "proj").Collect(context.Background(), "/p")
	if err != nil || len(signals) != 0 {
		t.Fatalf("query error must be best-effort; got (%+v, %v)", signals, err)
	}
}
