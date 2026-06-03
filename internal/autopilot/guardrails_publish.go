package autopilot

import (
	"context"
	"fmt"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
)

func (g *GuardrailsGate) blocking(violations []architect.Violation) bool {
	return g.cfg.blockingMode() && len(violations) > 0
}

func (g *GuardrailsGate) postStatus(ctx context.Context, headSHA string, violations []architect.Violation) {
	state := "success"
	if g.blocking(violations) {
		state = "failure"
	}
	status := &github.CommitStatus{
		State:       state,
		Context:     guardrailsStatusContext,
		Description: statusDescription(len(violations), g.cfg.EffectiveMode()),
	}
	if _, err := g.gh.CreateCommitStatus(ctx, g.owner, g.repo, headSHA, status); err != nil {
		g.log.Warn("guardrails: failed to post commit status", "sha", ShortSHA(headSHA), "error", err)
	}
}

func (g *GuardrailsGate) postComment(ctx context.Context, prNumber int, enforced, excepted []architect.Violation, usedExceptions []string, prior *github.Comment) {
	if len(enforced) == 0 && len(excepted) == 0 {
		return
	}
	body := renderGuardrailsComment(enforced, excepted, usedExceptions, g.cfg.EffectiveMode())
	if prior != nil {
		if _, err := g.gh.UpdateIssueComment(ctx, g.owner, g.repo, prior.ID, body); err != nil {
			g.log.Warn("guardrails: failed to update prior PR comment", "pr", prNumber, "comment", prior.ID, "error", err)
		}
		return
	}
	if _, err := g.gh.AddPRComment(ctx, g.owner, g.repo, prNumber, body); err != nil {
		g.log.Warn("guardrails: failed to post PR comment", "pr", prNumber, "error", err)
	}
}

func statusDescription(n int, mode string) string {
	if n == 0 {
		return "no architectural guardrail violations"
	}
	noun := "violation"
	if n != 1 {
		noun = "violations"
	}
	if mode == guardrailsModeBlock {
		return fmt.Sprintf("%d guardrail %s (blocking)", n, noun)
	}
	return fmt.Sprintf("%d guardrail %s (report-only)", n, noun)
}
