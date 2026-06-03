package architect

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// proposeTaskDescription is the task hint threaded into BuildGuidancePreamble so
// the .agent overlay machinery selects SOPs/context relevant to refactor work.
const proposeTaskDescription = "proactive refactor analysis"

// defaultProposeHeading and defaultProposeIntro are the prompt's framing for the
// generic (core/depdoctor) lenses. A LensSlant overrides them to aim the
// analysis at a specific concern.
const (
	defaultProposeHeading = "Proactive Refactor Analysis"
	defaultProposeIntro   = "A deterministic scan of this project produced the signals below. " +
		"Cluster related signals and propose the highest-value, smallest-blast-radius " +
		"changes. Rank them so the most important proposal comes first."
)

// maxSignalsInPrompt caps how many Signals are summarised into the prompt. The
// SCAN stage is uncapped (it may surface thousands of >400-LOC/TODO hits); the
// PROPOSE stage only needs the highest-weight ones to reason about, and an
// unbounded prompt wastes tokens and risks truncation. Signals beyond the cap
// are summarised as an aggregate tail count.
const maxSignalsInPrompt = 60

// backendFactory resolves a Backend from a stage + base config. It mirrors
// executor.NewStageBackend's signature so production code passes that function
// directly and tests inject a stub returning a mock Backend without spawning a
// real subprocess.
type backendFactory func(stage *executor.StageConfig, base executor.BackendConfig) (executor.Backend, error)

// Analyzer is the PROPOSE stage: a single configurable LLM pass that turns
// deterministic SCAN Signals into ranked pilotapi.Finding proposals. It owns the
// backend selection (stage + base config) and the .agent directory used to prime
// guidance, but performs no I/O until Propose is called.
type Analyzer struct {
	stage    *executor.StageConfig
	base     executor.BackendConfig
	agentDir string

	// slant optionally aims the PROPOSE prompt at a specific lens's concern
	// (e.g. the test-gap designer). Nil keeps the default refactor framing.
	slant *LensSlant

	// offline, when true, makes Propose synthesize findings deterministically
	// from the Signals instead of invoking the backend. It is the network-free
	// path the dry-run CLI uses so a scan produces real, graph-derived findings
	// without spawning an LLM subprocess.
	offline bool

	// newBackend resolves the backend for a Propose call. Defaults to
	// executor.NewStageBackend; tests override it to inject a mock.
	newBackend backendFactory
}

// AnalyzerOption configures an Analyzer at construction. Options keep the common
// NewAnalyzer call site unchanged while letting a lens-aware caller (the CLI)
// layer in a slant.
type AnalyzerOption func(*Analyzer)

// WithSlant aims the analyzer's PROPOSE prompt at a lens's concern. A nil slant
// is a no-op, so callers can pass a lens's (possibly absent) slant
// unconditionally.
func WithSlant(slant *LensSlant) AnalyzerOption {
	return func(a *Analyzer) { a.slant = slant }
}

// WithOffline toggles the deterministic, network-free PROPOSE path. When
// enabled, Propose synthesizes findings from the Signals directly (no backend
// subprocess, no LLM), making `pilot architect --dry-run` produce real
// graph-derived findings even without a reachable backend.
func WithOffline(offline bool) AnalyzerOption {
	return func(a *Analyzer) { a.offline = offline }
}

// NewAnalyzer builds an Analyzer that resolves its backend from stage (the
// per-stage override, may be nil) layered over base, and primes prompts with the
// guidance preamble loaded from agentDir's .agent context. Options (e.g.
// WithSlant) further tune the prompt.
func NewAnalyzer(stage *executor.StageConfig, base executor.BackendConfig, agentDir string, opts ...AnalyzerOption) *Analyzer {
	a := &Analyzer{
		stage:      stage,
		base:       base,
		agentDir:   agentDir,
		newBackend: executor.NewStageBackend,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Propose runs one backend pass over the given Signals and returns the ranked
// proposals the backend emitted. It is tolerant by design: an empty signal set
// yields an empty result without calling the backend; malformed backend output
// (fenced JSON, surrounding prose, invalid risk) is repaired where possible and
// dropped only when unrecoverable (missing title). A backend invocation error is
// propagated; a backend that simply returns no parseable JSON yields an empty,
// non-nil slice and no error.
func (a *Analyzer) Propose(ctx context.Context, signals []Signal) ([]pilotapi.Finding, error) {
	if len(signals) == 0 {
		return []pilotapi.Finding{}, nil
	}

	// Offline path: synthesize findings deterministically from the Signals,
	// never touching the backend. The finding framing is derived from each
	// Signal's Kind (which already encodes the lens's concern), so this honours
	// the active lens without needing the slant's prompt overrides.
	if a.offline {
		return SynthesizeFindings(signals), nil
	}

	prompt := a.buildPrompt(signals)

	backend, err := a.newBackend(a.stage, a.base)
	if err != nil {
		return nil, fmt.Errorf("architect: resolve propose backend: %w", err)
	}

	result, err := backend.Execute(ctx, executor.ExecuteOptions{
		Prompt:      prompt,
		ProjectPath: a.agentDir,
	})
	if err != nil {
		return nil, fmt.Errorf("architect: propose backend execute: %w", err)
	}
	if result == nil {
		return []pilotapi.Finding{}, nil
	}

	return parseFindings(result.Output), nil
}

// buildPrompt assembles the full PROPOSE prompt: the .agent guidance preamble,
// the compact signal summary, and the strict JSON-only instruction with the
// expected shape. The preamble is prepended so the backend sees Navigator/.agent
// context before the task, matching how the other backends are primed.
func (a *Analyzer) buildPrompt(signals []Signal) string {
	var b strings.Builder

	task := a.slant.taskDescriptionOr(proposeTaskDescription)
	if preamble := executor.BuildGuidancePreamble(a.agentDir, task); preamble != "" {
		b.WriteString(preamble)
		b.WriteString("\n\n")
	}

	b.WriteString("# ")
	b.WriteString(a.slant.headingOr(defaultProposeHeading))
	b.WriteString("\n\n")
	b.WriteString(a.slant.introOr(defaultProposeIntro))
	b.WriteString("\n\n## Signals\n\n")
	b.WriteString(summarizeSignals(signals))
	if extra := a.slant.extraInstruction(); extra != "" {
		b.WriteString("\n\n## Focus\n\n")
		b.WriteString(extra)
	}
	b.WriteString("\n\n## Required Output\n\n")
	b.WriteString("Reply with ONLY a JSON array of proposal objects and nothing else — ")
	b.WriteString("no prose, no markdown fences. Each object must match this shape exactly:\n\n")
	b.WriteString(proposalJSONShape)
	b.WriteString("\n")

	return b.String()
}

// summarizeSignals renders Signals into a compact, deterministic bullet list,
// highest-weight first. It caps the body at maxSignalsInPrompt entries and, when
// truncated, appends an aggregate tail line so the model still knows the total
// volume of lower-weight signals it is not seeing individually.
func summarizeSignals(signals []Signal) string {
	ordered := make([]Signal, len(signals))
	copy(ordered, signals)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Weight > ordered[j].Weight
	})

	shown := ordered
	tail := 0
	if len(ordered) > maxSignalsInPrompt {
		shown = ordered[:maxSignalsInPrompt]
		tail = len(ordered) - maxSignalsInPrompt
	}

	var b strings.Builder
	for _, s := range shown {
		b.WriteString("- ")
		b.WriteString(signalLine(s))
		b.WriteString("\n")
	}
	if tail > 0 {
		fmt.Fprintf(&b, "- (+%d more lower-weight signals omitted)\n", tail)
	}
	return strings.TrimRight(b.String(), "\n")
}

// signalLine renders one Signal as a single descriptive line. Location is
// included only when present so line-agnostic signals stay terse.
func signalLine(s Signal) string {
	risk := s.Risk
	if risk == "" {
		risk = pilotapi.RiskMedium
	}

	loc := s.File
	if loc != "" && s.Line > 0 {
		loc = fmt.Sprintf("%s:%d", s.File, s.Line)
	}

	parts := []string{fmt.Sprintf("[%s]", s.Kind)}
	if loc != "" {
		parts = append(parts, loc)
	}
	if s.Detail != "" {
		parts = append(parts, s.Detail)
	}
	parts = append(parts, fmt.Sprintf("(risk=%s, weight=%.2f)", risk, s.Weight))
	return strings.Join(parts, " ")
}

// parseFindings tolerantly decodes the backend's output into a ranked slice of
// proposals. It extracts the JSON array, unmarshals it, normalises each Risk and
// trims whitespace, and drops proposals with no usable Title. Unparseable or
// empty output yields a non-nil, zero-length slice — never an error or a panic.
func parseFindings(output string) []pilotapi.Finding {
	body := extractJSON(output)
	if body == "" {
		return []pilotapi.Finding{}
	}

	var raw []pilotapi.Finding
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return []pilotapi.Finding{}
	}

	out := make([]pilotapi.Finding, 0, len(raw))
	for _, f := range raw {
		f.Title = strings.TrimSpace(f.Title)
		if f.Title == "" {
			continue
		}
		f.Risk = normalizeRisk(f.Risk)
		out = append(out, f)
	}
	return out
}
