package architect

import (
	"context"
	"errors"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// stubCollector is a programmable Collector for Scanner tests.
type stubCollector struct {
	name    string
	signals []Signal
	err     error
	calls   *int
}

func (s *stubCollector) Name() string { return s.name }

func (s *stubCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	if s.calls != nil {
		*s.calls++
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.signals, nil
}

func TestScanner_AggregatesInOrder(t *testing.T) {
	a := &stubCollector{name: "a", signals: []Signal{{Kind: "a1"}, {Kind: "a2"}}}
	b := &stubCollector{name: "b", signals: []Signal{{Kind: "b1"}}}

	s := NewScanner(a, b)
	got, err := s.Scan(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("Scan: unexpected error: %v", err)
	}
	want := []string{"a1", "a2", "b1"}
	if len(got) != len(want) {
		t.Fatalf("got %d signals, want %d: %+v", len(got), len(want), got)
	}
	for i, k := range want {
		if got[i].Kind != k {
			t.Errorf("signal[%d].Kind = %q, want %q", i, got[i].Kind, k)
		}
	}
}

func TestScanner_ToleratesCollectorError(t *testing.T) {
	bCalls := 0
	a := &stubCollector{name: "a", err: errors.New("boom")}
	b := &stubCollector{name: "b", signals: []Signal{{Kind: "b1"}}, calls: &bCalls}

	s := NewScanner(a, b)
	got, err := s.Scan(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("Scan should tolerate collector error, got: %v", err)
	}
	if bCalls != 1 {
		t.Fatalf("collector after failing one should still run; calls=%d", bCalls)
	}
	if len(got) != 1 || got[0].Kind != "b1" {
		t.Fatalf("expected only b1 signal, got %+v", got)
	}
}

func TestScanner_EmptyRegistryReturnsNonNilEmpty(t *testing.T) {
	s := NewScanner()
	got, err := s.Scan(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("Scan result must be non-nil even with no collectors")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 signals, got %d", len(got))
	}
}

func TestScanner_AllCollectorsFailReturnsEmpty(t *testing.T) {
	a := &stubCollector{name: "a", err: errors.New("x")}
	b := &stubCollector{name: "b", err: errors.New("y")}
	s := NewScanner(a, b)
	got, err := s.Scan(context.Background(), "/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 signals when all collectors fail, got %+v", got)
	}
}

func TestScanner_CancelledContextStopsEarly(t *testing.T) {
	calls := 0
	a := &stubCollector{name: "a", signals: []Signal{{Kind: "a1"}}, calls: &calls}
	s := NewScanner(a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := s.Scan(ctx, "/proj")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("collectors must not run after cancellation; calls=%d", calls)
	}
	if got == nil {
		t.Fatal("result must be non-nil")
	}
}

func TestScanner_CollectorsReturnsCopy(t *testing.T) {
	a := &stubCollector{name: "a"}
	s := NewScanner(a)
	got := s.Collectors()
	if len(got) != 1 {
		t.Fatalf("expected 1 collector, got %d", len(got))
	}
	got[0] = &stubCollector{name: "mutated"}
	if s.Collectors()[0].Name() != "a" {
		t.Fatal("mutating returned slice must not affect Scanner registry")
	}
}

func TestSignal_RiskFieldUsesPilotapi(t *testing.T) {
	sig := Signal{Risk: pilotapi.RiskHigh}
	if !sig.Risk.IsValid() {
		t.Fatal("Signal.Risk should accept canonical pilotapi RiskLevel")
	}
}
