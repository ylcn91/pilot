package architect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// mockBackend is a controllable executor.Backend for PROPOSE tests. It records
// the ExecuteOptions it was called with so tests can assert prompt contents, and
// returns a fixed output/error so no real subprocess is ever spawned.
type mockBackend struct {
	output    string
	err       error
	calls     int
	lastOpts  executor.ExecuteOptions
	nilResult bool
	available bool
}

func (m *mockBackend) Name() string      { return "mock" }
func (m *mockBackend) IsAvailable() bool { return m.available }

func (m *mockBackend) Execute(_ context.Context, opts executor.ExecuteOptions) (*executor.BackendResult, error) {
	m.calls++
	m.lastOpts = opts
	if m.err != nil {
		return nil, m.err
	}
	if m.nilResult {
		return nil, nil
	}
	return &executor.BackendResult{Success: true, Output: m.output}, nil
}

// newTestAnalyzer wires an Analyzer whose backend factory always returns mb,
// bypassing executor.NewStageBackend so tests never depend on a real CLI.
func newTestAnalyzer(mb *mockBackend) *Analyzer {
	a := NewAnalyzer(nil, executor.BackendConfig{}, "")
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}
	return a
}

func sampleSignals() []Signal {
	return []Signal{
		{Kind: "loc_over_400", File: "internal/big.go", Line: 0, Detail: "612 LOC", Weight: 3.0, Risk: pilotapi.RiskHigh},
		{Kind: "todo_fixme", File: "internal/api.go", Line: 42, Detail: "FIXME: race", Weight: 1.5, Risk: pilotapi.RiskMedium},
	}
}

func TestProposeCleanJSON(t *testing.T) {
	mb := &mockBackend{output: `[
	  {"title":"Split big.go","kind":"refactor","risk":"high","why_it_matters":"too long","suggested_pr_pieces":["extract helpers"],"test_plan":"go test","files":["internal/big.go"]}
	]`}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	f := got[0]
	if f.Title != "Split big.go" {
		t.Errorf("Title = %q", f.Title)
	}
	if f.Risk != pilotapi.RiskHigh {
		t.Errorf("Risk = %q, want high", f.Risk)
	}
	if len(f.Files) != 1 || f.Files[0] != "internal/big.go" {
		t.Errorf("Files = %v", f.Files)
	}
	if mb.calls != 1 {
		t.Errorf("backend called %d times, want 1", mb.calls)
	}
}

func TestProposeFencedJSON(t *testing.T) {
	mb := &mockBackend{output: "```json\n[{\"title\":\"T\",\"risk\":\"low\"}]\n```"}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if len(got) != 1 || got[0].Title != "T" || got[0].Risk != pilotapi.RiskLow {
		t.Fatalf("got %+v, want one low-risk finding 'T'", got)
	}
}

func TestProposeSurroundingProse(t *testing.T) {
	mb := &mockBackend{output: "Sure, here you go:\n[{\"title\":\"P\",\"risk\":\"medium\"}]\nHope that helps!"}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if len(got) != 1 || got[0].Title != "P" {
		t.Fatalf("got %+v, want one finding 'P'", got)
	}
}

func TestProposeInvalidRiskNormalized(t *testing.T) {
	mb := &mockBackend{output: `[{"title":"X","risk":"catastrophic"}]`}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Risk != pilotapi.RiskMedium {
		t.Errorf("invalid risk normalized to %q, want medium", got[0].Risk)
	}
}

func TestProposeMissingTitleDropped(t *testing.T) {
	mb := &mockBackend{output: `[
	  {"title":"","risk":"low"},
	  {"title":"   ","risk":"low"},
	  {"title":"Keep me","risk":"low"}
	]`}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1 (blank titles dropped)", len(got))
	}
	if got[0].Title != "Keep me" {
		t.Errorf("kept Title = %q, want 'Keep me'", got[0].Title)
	}
}

func TestProposeTitleWhitespaceTrimmed(t *testing.T) {
	mb := &mockBackend{output: `[{"title":"  Trim me  ","risk":"low"}]`}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Trim me" {
		t.Fatalf("got %+v, want trimmed title 'Trim me'", got)
	}
}

func TestProposeEmptyOutput(t *testing.T) {
	mb := &mockBackend{output: ""}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d findings, want 0", len(got))
	}
}

func TestProposeGarbageOutput(t *testing.T) {
	for _, out := range []string{
		"I could not complete this task.",
		"```\nnot json\n```",
		"[ this is { not ] valid json",
		"{\"title\":\"object not array\"}",
	} {
		mb := &mockBackend{output: out}
		a := newTestAnalyzer(mb)
		got, err := a.Propose(context.Background(), sampleSignals())
		if err != nil {
			t.Errorf("Propose(%q) error: %v", out, err)
			continue
		}
		if got == nil || len(got) != 0 {
			t.Errorf("Propose(%q) = %+v, want empty non-nil slice", out, got)
		}
	}
}

func TestProposeBackendErrorPropagated(t *testing.T) {
	sentinel := errors.New("backend exploded")
	mb := &mockBackend{err: sentinel}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err == nil {
		t.Fatal("Propose returned nil error, want propagated backend error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error %v does not wrap sentinel", err)
	}
	if got != nil {
		t.Errorf("got %+v on error, want nil", got)
	}
}

func TestProposeBackendFactoryErrorPropagated(t *testing.T) {
	factoryErr := errors.New("no such backend")
	a := NewAnalyzer(nil, executor.BackendConfig{}, "")
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return nil, factoryErr
	}

	got, err := a.Propose(context.Background(), sampleSignals())
	if err == nil {
		t.Fatal("Propose returned nil error, want factory error")
	}
	if !errors.Is(err, factoryErr) {
		t.Errorf("error %v does not wrap factory error", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil on factory error", got)
	}
}

func TestProposeNilBackendResult(t *testing.T) {
	mb := &mockBackend{nilResult: true}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %+v, want empty non-nil slice", got)
	}
}

func TestProposeEmptySignalsSkipsBackend(t *testing.T) {
	mb := &mockBackend{output: `[{"title":"should not appear"}]`}
	a := newTestAnalyzer(mb)

	got, err := a.Propose(context.Background(), nil)
	if err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %+v, want empty non-nil slice", got)
	}
	if mb.calls != 0 {
		t.Errorf("backend called %d times for empty signals, want 0", mb.calls)
	}
}

func TestProposePromptIncludesSignalSummary(t *testing.T) {
	mb := &mockBackend{output: "[]"}
	a := newTestAnalyzer(mb)

	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose error: %v", err)
	}

	prompt := mb.lastOpts.Prompt
	for _, want := range []string{
		"loc_over_400",
		"internal/big.go",
		"todo_fixme",
		"internal/api.go:42",
		"FIXME: race",
		"# Proactive Refactor Analysis",
		"JSON array",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, prompt)
		}
	}
}

func TestProposePromptIncludesGuidancePreamble(t *testing.T) {
	mb := &mockBackend{output: "[]"}
	a := newTestAnalyzer(mb)

	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose error: %v", err)
	}

	// BuildGuidancePreamble falls back to the executor prompt-header const even
	// with an empty agentDir, so the preamble marker must lead the prompt and
	// precede the analysis body.
	prompt := mb.lastOpts.Prompt
	wantPreamble := executor.BuildGuidancePreamble("", proposeTaskDescription)
	if wantPreamble == "" {
		t.Fatal("test precondition: guidance preamble unexpectedly empty")
	}
	if !strings.Contains(prompt, wantPreamble) {
		t.Errorf("prompt missing guidance preamble\n--- prompt ---\n%s", prompt)
	}
	pre := strings.Index(prompt, wantPreamble)
	body := strings.Index(prompt, "# Proactive Refactor Analysis")
	if pre < 0 || body < 0 || pre > body {
		t.Errorf("preamble (%d) must precede analysis body (%d)", pre, body)
	}
}

func TestProposePromptIncludesJSONShape(t *testing.T) {
	mb := &mockBackend{output: "[]"}
	a := newTestAnalyzer(mb)

	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	for _, field := range []string{"why_it_matters", "suggested_pr_pieces", "test_plan"} {
		if !strings.Contains(mb.lastOpts.Prompt, field) {
			t.Errorf("prompt missing JSON-shape field %q", field)
		}
	}
}

func TestProposeRanksByWeightInPrompt(t *testing.T) {
	mb := &mockBackend{output: "[]"}
	a := newTestAnalyzer(mb)

	signals := []Signal{
		{Kind: "low_weight", Detail: "minor", Weight: 0.1},
		{Kind: "high_weight", Detail: "major", Weight: 9.9},
	}
	if _, err := a.Propose(context.Background(), signals); err != nil {
		t.Fatalf("Propose error: %v", err)
	}

	hi := strings.Index(mb.lastOpts.Prompt, "high_weight")
	lo := strings.Index(mb.lastOpts.Prompt, "low_weight")
	if hi < 0 || lo < 0 {
		t.Fatalf("both signals must appear; hi=%d lo=%d", hi, lo)
	}
	if hi > lo {
		t.Errorf("high-weight signal should precede low-weight in prompt (hi=%d, lo=%d)", hi, lo)
	}
}

func TestProposeProjectPathIsAgentDir(t *testing.T) {
	mb := &mockBackend{output: "[]"}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "/tmp/proj")
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}
	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose error: %v", err)
	}
	if mb.lastOpts.ProjectPath != "/tmp/proj" {
		t.Errorf("ProjectPath = %q, want /tmp/proj", mb.lastOpts.ProjectPath)
	}
}
