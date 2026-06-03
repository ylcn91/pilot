package architect

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
)

func TestLensSlant_DefaultsWhenNil(t *testing.T) {
	var s *LensSlant
	if got := s.taskDescriptionOr("fallback-task"); got != "fallback-task" {
		t.Errorf("nil slant task = %q, want fallback-task", got)
	}
	if got := s.headingOr("fallback-heading"); got != "fallback-heading" {
		t.Errorf("nil slant heading = %q, want fallback-heading", got)
	}
	if got := s.introOr("fallback-intro"); got != "fallback-intro" {
		t.Errorf("nil slant intro = %q, want fallback-intro", got)
	}
	if got := s.extraInstruction(); got != "" {
		t.Errorf("nil slant extra = %q, want empty", got)
	}
}

func TestLensSlant_BlankFieldsFallBack(t *testing.T) {
	s := &LensSlant{TaskDescription: "   ", Heading: "", Intro: "  "}
	if s.taskDescriptionOr("t") != "t" {
		t.Error("blank task must fall back")
	}
	if s.headingOr("h") != "h" {
		t.Error("blank heading must fall back")
	}
	if s.introOr("i") != "i" {
		t.Error("blank intro must fall back")
	}
}

func TestLensSlant_OverridesWhenSet(t *testing.T) {
	s := &LensSlant{
		TaskDescription:  "design tests",
		Heading:          "Test-Gap Designer",
		Intro:            "find the gaps",
		ExtraInstruction: "  build a matrix  ",
	}
	if s.taskDescriptionOr("x") != "design tests" {
		t.Error("set task must win")
	}
	if s.headingOr("x") != "Test-Gap Designer" {
		t.Error("set heading must win")
	}
	if s.introOr("x") != "find the gaps" {
		t.Error("set intro must win")
	}
	if s.extraInstruction() != "build a matrix" {
		t.Errorf("extra instruction must be trimmed, got %q", s.extraInstruction())
	}
}

// TestAnalyzer_NoSlantKeepsDefaultPrompt proves the no-slant path is unchanged:
// the default heading/intro appear and no Focus section is injected.
func TestAnalyzer_NoSlantKeepsDefaultPrompt(t *testing.T) {
	mb := &mockBackend{output: "[]"}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "")
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}
	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	p := mb.lastOpts.Prompt
	if !strings.Contains(p, defaultProposeHeading) {
		t.Errorf("default heading missing from prompt")
	}
	if !strings.Contains(p, "smallest-blast-radius") {
		t.Errorf("default refactor intro missing from prompt")
	}
	if strings.Contains(p, "## Focus") {
		t.Errorf("no-slant prompt must not inject a Focus section")
	}
}

// TestAnalyzer_WithSlantAimsPrompt proves WithSlant replaces the framing and
// injects the lens-specific Focus block.
func TestAnalyzer_WithSlantAimsPrompt(t *testing.T) {
	slant := &LensSlant{
		TaskDescription:  "design a missing-test matrix",
		Heading:          "Test-Gap Designer",
		Intro:            "coverage, not refactoring",
		ExtraInstruction: "produce a missing-test MATRIX",
	}
	mb := &mockBackend{output: "[]"}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "", WithSlant(slant))
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}
	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	p := mb.lastOpts.Prompt
	for _, want := range []string{"Test-Gap Designer", "coverage, not refactoring", "## Focus", "missing-test MATRIX"} {
		if !strings.Contains(p, want) {
			t.Errorf("slanted prompt missing %q", want)
		}
	}
	if strings.Contains(p, defaultProposeHeading) {
		t.Errorf("slant must replace the default heading, but it is still present")
	}
	// The output contract is unchanged regardless of slant.
	if !strings.Contains(p, proposalJSONShape) {
		t.Errorf("slanted prompt must keep the JSON output contract")
	}
}

func TestAnalyzer_NilSlantOptionIsNoOp(t *testing.T) {
	a := NewAnalyzer(nil, executor.BackendConfig{}, "", WithSlant(nil))
	if a.slant != nil {
		t.Fatal("WithSlant(nil) must leave the analyzer slant nil")
	}
}
