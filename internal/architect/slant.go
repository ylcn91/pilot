package architect

import "strings"

// LensSlant aims the PROPOSE stage at a specific lens's concern. The default
// PROPOSE prompt asks the backend for generic "smallest-blast-radius refactor"
// proposals; a slant replaces that framing so a specialised lens (e.g. the
// test-gap designer) gets proposals shaped to its goal — for the test-gap lens,
// a missing-test matrix and critical-path coverage plan rather than a refactor.
//
// A slant changes only the prompt's framing and the task hint fed to the
// .agent guidance overlay. It does not change the output contract: the backend
// still replies with the same pilotapi.Finding JSON array, so the EMIT stage is
// lens-agnostic.
type LensSlant struct {
	// TaskDescription is the hint threaded into BuildGuidancePreamble so the
	// .agent overlay selects SOPs/context relevant to this lens's concern. When
	// empty the analyzer keeps its default task description.
	TaskDescription string

	// Heading replaces the prompt's top-level section heading (e.g. "Test-Gap
	// Designer" instead of "Proactive Refactor Analysis"). When empty the
	// analyzer keeps its default heading.
	Heading string

	// Intro replaces the prompt's framing paragraph that tells the backend what
	// kind of proposals to produce from the signals. When empty the analyzer
	// keeps its default refactor framing.
	Intro string

	// ExtraInstruction, when non-empty, is appended after the signal summary and
	// before the required-output contract. A lens uses it to demand
	// lens-specific payload (for the test-gap lens: a missing-test matrix, a
	// critical-path list, race/e2e suggestions, and a fixture-reuse plan, all
	// carried in each finding's test_plan).
	ExtraInstruction string
}

// taskDescriptionOr returns the slant's task description, or fallback when the
// slant is nil or its description is blank.
func (s *LensSlant) taskDescriptionOr(fallback string) string {
	if s == nil || strings.TrimSpace(s.TaskDescription) == "" {
		return fallback
	}
	return s.TaskDescription
}

// headingOr returns the slant's heading, or fallback when unset.
func (s *LensSlant) headingOr(fallback string) string {
	if s == nil || strings.TrimSpace(s.Heading) == "" {
		return fallback
	}
	return s.Heading
}

// introOr returns the slant's intro framing, or fallback when unset.
func (s *LensSlant) introOr(fallback string) string {
	if s == nil || strings.TrimSpace(s.Intro) == "" {
		return fallback
	}
	return s.Intro
}

// extraInstruction returns the slant's extra instruction block, or the empty
// string when the slant is nil or carries none.
func (s *LensSlant) extraInstruction() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.ExtraInstruction)
}
