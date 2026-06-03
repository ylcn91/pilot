package autopilot

import (
	"context"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// runGuardrailsGate runs the optional per-PR architectural guardrails gate as a
// fail-open side effect of handleCIPassed. It never changes PR state; blocking is
// expressed only through the pilot/guardrails commit status.
func (c *Controller) runGuardrailsGate(ctx context.Context, prState *PRState, files []*github.PRFile) {
	if c.guardrailsGate == nil || !c.guardrailsGate.Enabled() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			c.log.Warn("guardrails gate panicked, ignoring (fail-open)",
				"pr", prState.PRNumber, "panic", r)
		}
	}()
	violations, err := c.guardrailsGate.EvaluateWithFiles(ctx, prState.PRNumber, prState.HeadSHA, files)
	if err != nil {
		c.log.Warn("guardrails gate errored, continuing (fail-open)",
			"pr", prState.PRNumber, "error", err)
		return
	}
	if len(violations) > 0 {
		c.log.Info("guardrails gate found violations (report-only unless block mode)",
			"pr", prState.PRNumber, "count", len(violations))
	}
}
