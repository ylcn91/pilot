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

// mockPitfallSource is a programmable pitfallSource.
type mockPitfallSource struct {
	mems    []*memory.Memory
	err     error
	gotType memory.MemoryType
}

func (m *mockPitfallSource) QueryByType(t memory.MemoryType, projectID string) ([]*memory.Memory, error) {
	m.gotType = t
	return m.mems, m.err
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

func TestPitfallCollector_EmitsPerPitfall(t *testing.T) {
	src := &mockPitfallSource{mems: []*memory.Memory{
		{Content: "auth changes break tests", Context: "internal/auth", Confidence: 0.9},
		{Content: "migration order matters", Context: "db", Confidence: 0.5},
		nil, // tolerated
	}}
	c := NewPitfallCollector(src, "proj")
	signals, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if src.gotType != memory.MemoryTypePitfall {
		t.Errorf("queried type %q, want pitfall", src.gotType)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 pitfall signals, got %d: %+v", len(signals), signals)
	}
	if signals[0].Kind != "known_pitfall" {
		t.Errorf("Kind = %q, want known_pitfall", signals[0].Kind)
	}
	if signals[0].File != "internal/auth" {
		t.Errorf("File = %q, want internal/auth", signals[0].File)
	}
	if signals[0].Weight != 0.9 {
		t.Errorf("Weight should equal confidence 0.9, got %v", signals[0].Weight)
	}
	if signals[0].Risk != pilotapi.RiskMedium {
		t.Errorf("Risk = %q, want medium", signals[0].Risk)
	}
}

func TestPitfallCollector_QueryErrorBestEffort(t *testing.T) {
	src := &mockPitfallSource{err: errors.New("boom")}
	signals, err := NewPitfallCollector(src, "proj").Collect(context.Background(), "/p")
	if err != nil || len(signals) != 0 {
		t.Fatalf("query error must be best-effort; got (%+v, %v)", signals, err)
	}
}

func TestPitfallCollector_NilSourceInert(t *testing.T) {
	signals, err := NewPitfallCollector(nil, "proj").Collect(context.Background(), "/p")
	if err != nil || signals != nil {
		t.Fatalf("nil source must be inert; got (%+v, %v)", signals, err)
	}
}

func TestPitfallCollector_EmptyResult(t *testing.T) {
	signals, err := NewPitfallCollector(&mockPitfallSource{}, "proj").Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("no pitfalls => no signals; got %+v", signals)
	}
}
