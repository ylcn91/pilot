package pilotapi

import "time"

// QualityGateDetail represents detailed information about a single gate check.
// It is part of the neutral handoff contract so that the quality subsystem can
// report gate results and the executor can consume them without either package
// depending on the other's concrete types.
type QualityGateDetail struct {
	Name       string
	Passed     bool
	Duration   time.Duration
	RetryCount int
	Error      string
}

// QualityOutcome represents the result of quality gate checks. It is the
// flattened, package-neutral form of a quality run handed from the quality
// subsystem to the executor's retry loop.
type QualityOutcome struct {
	Passed        bool
	ShouldRetry   bool
	RetryFeedback string // Error feedback to send to Claude for retry
	Attempt       int
	// GateDetails contains detailed results for each gate
	GateDetails []QualityGateDetail
	// TotalDuration is the total time spent running all gates
	TotalDuration time.Duration
}
