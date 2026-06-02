package architect

import (
	"context"
	"errors"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
	"github.com/ylcn91/pilot/internal/quality"
)

// mockGateRunner is a programmable gateRunner for collector tests.
type mockGateRunner struct {
	result  *quality.Result
	err     error
	gotGate string
}

func (m *mockGateRunner) RunGate(ctx context.Context, gateName string) (*quality.Result, error) {
	m.gotGate = gateName
	return m.result, m.err
}

func TestLintCollector_ParsesFindings(t *testing.T) {
	out := `internal/foo/bar.go:12:5: ineffectual assignment to err (ineffassign)
internal/foo/bar.go:30: missing return
some banner line that is not a diagnostic
Running golangci-lint...`
	m := &mockGateRunner{result: &quality.Result{Status: quality.StatusFailed, Output: out}}

	signals, err := NewLintCollector(m, "lint").Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if m.gotGate != "lint" {
		t.Errorf("ran gate %q, want lint", m.gotGate)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 parsed findings, got %d: %+v", len(signals), signals)
	}
	if signals[0].File != "internal/foo/bar.go" || signals[0].Line != 12 {
		t.Errorf("first finding location wrong: %+v", signals[0])
	}
	if signals[1].Line != 30 {
		t.Errorf("second finding line = %d, want 30", signals[1].Line)
	}
	for _, s := range signals {
		if s.Kind != "lint" || s.Risk != pilotapi.RiskLow {
			t.Errorf("unexpected kind/risk: %+v", s)
		}
	}
}

func TestLintCollector_PassedGateNoSignals(t *testing.T) {
	m := &mockGateRunner{result: &quality.Result{Status: quality.StatusPassed}}
	signals, err := NewLintCollector(m, "lint").Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("passing lint gate must yield no signals; got %+v", signals)
	}
}

func TestLintCollector_MissingToolIsBestEffort(t *testing.T) {
	// Simulate golangci-lint absent: RunGate errors.
	m := &mockGateRunner{err: errors.New("exec: golangci-lint not found")}
	signals, err := NewLintCollector(m, "lint").Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("lint collector must not error when tool absent; got %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected zero signals when tool absent; got %+v", signals)
	}
}

func TestLintCollector_NilRunnerInert(t *testing.T) {
	signals, err := NewLintCollector(nil, "lint").Collect(context.Background(), "/p")
	if err != nil || signals != nil {
		t.Fatalf("nil runner must be inert; got (%+v, %v)", signals, err)
	}
}

func TestLintCollector_NilResultNoError(t *testing.T) {
	m := &mockGateRunner{result: nil, err: nil}
	signals, err := NewLintCollector(m, "lint").Collect(context.Background(), "/p")
	if err != nil || len(signals) != 0 {
		t.Fatalf("nil result must yield no signals and no error; got (%+v, %v)", signals, err)
	}
}

func TestCoverageCollector_BelowThresholdEmits(t *testing.T) {
	m := &mockGateRunner{result: &quality.Result{Status: quality.StatusPassed, Coverage: 40}}
	signals, err := NewCoverageCollector(m, "coverage", 80).Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 low_coverage signal, got %d", len(signals))
	}
	s := signals[0]
	if s.Kind != "low_coverage" {
		t.Errorf("Kind = %q, want low_coverage", s.Kind)
	}
	// 40 < 80/2 => high risk
	if s.Risk != pilotapi.RiskHigh {
		t.Errorf("Risk = %q, want high for coverage 40 vs threshold 80", s.Risk)
	}
	if s.Weight <= 0 {
		t.Errorf("Weight should be > 0 for a shortfall, got %v", s.Weight)
	}
}

func TestCoverageCollector_AtOrAboveThresholdNoSignal(t *testing.T) {
	for _, cov := range []float64{80, 95} {
		m := &mockGateRunner{result: &quality.Result{Coverage: cov}}
		signals, err := NewCoverageCollector(m, "coverage", 80).Collect(context.Background(), "/p")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(signals) != 0 {
			t.Fatalf("coverage %.0f >= threshold must yield no signal; got %+v", cov, signals)
		}
	}
}

func TestCoverageCollector_MediumRiskForModestShortfall(t *testing.T) {
	m := &mockGateRunner{result: &quality.Result{Coverage: 70}}
	signals, err := NewCoverageCollector(m, "coverage", 80).Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(signals) != 1 || signals[0].Risk != pilotapi.RiskMedium {
		t.Fatalf("coverage 70 vs 80 should be medium risk; got %+v", signals)
	}
}

func TestCoverageCollector_RunErrorIsBestEffort(t *testing.T) {
	m := &mockGateRunner{err: errors.New("no go test")}
	signals, err := NewCoverageCollector(m, "coverage", 80).Collect(context.Background(), "/p")
	if err != nil || len(signals) != 0 {
		t.Fatalf("run error must be best-effort; got (%+v, %v)", signals, err)
	}
}

func TestCoverageCollector_NilRunnerInert(t *testing.T) {
	signals, err := NewCoverageCollector(nil, "coverage", 80).Collect(context.Background(), "/p")
	if err != nil || signals != nil {
		t.Fatalf("nil runner must be inert; got (%+v, %v)", signals, err)
	}
}

func TestParseLintOutput_IgnoresNoise(t *testing.T) {
	signals := parseLintOutput("just a summary\n0 issues\n")
	if len(signals) != 0 {
		t.Fatalf("non-diagnostic lines must produce no signals; got %+v", signals)
	}
}

func TestAtoiSafe(t *testing.T) {
	cases := map[string]int{"0": 0, "42": 42, "x": 0, "": 0, "12a": 0}
	for in, want := range cases {
		if got := atoiSafe(in); got != want {
			t.Errorf("atoiSafe(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCoverageWeight(t *testing.T) {
	if w := coverageWeight(80, 80); w != 0 {
		t.Errorf("no shortfall => weight 0, got %v", w)
	}
	if w := coverageWeight(40, 80); w != 0.5 {
		t.Errorf("coverageWeight(40,80) = %v, want 0.5", w)
	}
	if w := coverageWeight(40, 0); w != 0 {
		t.Errorf("zero threshold => weight 0, got %v", w)
	}
}
