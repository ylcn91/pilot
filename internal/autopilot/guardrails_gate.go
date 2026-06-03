package autopilot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
)

// Guardrails mode constants. These mirror the config package's report/block
// values, redeclared here so the autopilot package stays free of an import of
// config (config imports autopilot, so the reverse edge would be a cycle). The
// caller that owns *config.GuardrailsConfig translates it into a
// GuardrailsGateConfig at wiring time.
const (
	guardrailsModeReport = "report"
	guardrailsModeBlock  = "block"
)

// guardrailsStatusContext is the commit-status context the gate posts under. It
// is namespaced so it never collides with CI or the executor's own
// "pilot/execution" status, and so branch protection can target it by name.
const guardrailsStatusContext = "pilot/guardrails"

// guardrailsCommentMarker is embedded (invisibly) in the PR comment so a repeat
// run can recognise its own prior comment instead of stacking duplicates. The
// gate is otherwise stateless, so this marker is the only idempotency signal.
const guardrailsCommentMarker = "<!-- pilot-guardrails -->"

// GuardrailsGateConfig is the resolved, package-local view of the guardrails
// feature flags the gate needs. It deliberately duplicates the three relevant
// fields of config.GuardrailsConfig rather than importing that type, because
// config imports autopilot and the reverse edge is forbidden.
//
// The zero value is inert and fail-open: Enabled is false (gate is a no-op) and
// an empty Mode resolves to report-only.
type GuardrailsGateConfig struct {
	// Enabled gates the whole feature. False => the gate does nothing.
	Enabled bool
	// Mode is "report" (default) or "block". Empty means report.
	Mode string
	// DisabledRules are rule names to skip (passed straight to the registry).
	DisabledRules []string
}

// EffectiveMode resolves an empty Mode to the report-only default.
func (c GuardrailsGateConfig) EffectiveMode() string {
	if c.Mode == "" {
		return guardrailsModeReport
	}
	return c.Mode
}

// blockingMode reports whether the config is enabled and in block mode. A run
// only blocks (posts a failure status) when this is true AND there is at least
// one violation; see GuardrailsGate.blocking.
func (c GuardrailsGateConfig) blockingMode() bool {
	return c.Enabled && c.Mode == guardrailsModeBlock
}

// guardrailsGitHub is the slice of the GitHub client the guardrails gate needs.
// *github.Client satisfies it; tests substitute a mock to assert exactly what
// the gate posts. Keeping the surface this small means the gate cannot reach
// for any mutating call (merge, label, close) beyond status + comment.
type guardrailsGitHub interface {
	ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]*github.PRFile, error)
	CreateCommitStatus(ctx context.Context, owner, repo, sha string, status *github.CommitStatus) (*github.CommitStatus, error)
	AddPRComment(ctx context.Context, owner, repo string, number int, body string) (*github.PRComment, error)
}

// ruleEvaluator is the architect rule registry surface the gate depends on,
// extracted as an interface so the gate can be unit-tested with a stub registry
// that plants deterministic violations without a real worktree.
type ruleEvaluator interface {
	Evaluate(ctx context.Context, changedFiles []string, worktreePath string, disabled []string) []architect.Violation
}

// GuardrailsGate evaluates the architect guardrail rules against a single PR's
// changed files and surfaces the result as a commit status plus (when there are
// findings) a PR comment.
//
// It is fail-open and report-only by construction:
//
//   - Disabled config (the default) => Evaluate is a no-op; existing autopilot
//     behaviour is untouched.
//   - Any GitHub or evaluation error => the gate logs and returns nil; it never
//     propagates an error that could fail a PR, and it never posts a failure
//     status it didn't mean to.
//   - The commit status is "failure" ONLY when the config is explicitly in
//     "block" mode AND at least one violation was found. In the default "report"
//     mode the status is always "success" regardless of findings, so turning the
//     feature on can only add information, never hold a PR.
type GuardrailsGate struct {
	gh           guardrailsGitHub
	registry     ruleEvaluator
	cfg          GuardrailsGateConfig
	worktreePath string
	owner        string
	repo         string
	log          *slog.Logger
}

// NewGuardrailsGate builds a gate. worktreePath is the checkout the rules read
// (the local clone of the PR's head); when empty the rules degrade to no
// findings, keeping the gate fail-open.
func NewGuardrailsGate(gh guardrailsGitHub, registry ruleEvaluator, cfg GuardrailsGateConfig, worktreePath, owner, repo string) *GuardrailsGate {
	return &GuardrailsGate{
		gh:           gh,
		registry:     registry,
		cfg:          cfg,
		worktreePath: worktreePath,
		owner:        owner,
		repo:         repo,
		log:          slog.Default().With("component", "guardrails-gate"),
	}
}

// Enabled reports whether the gate will do anything for a PR. A nil gate or a
// disabled config is inert.
func (g *GuardrailsGate) Enabled() bool {
	return g != nil && g.cfg.Enabled
}

// Evaluate runs the guardrail rules over the PR's changed files and posts a
// commit status (always) and a PR comment (only when there are violations).
//
// It returns the violations it found purely for caller observability/tests; the
// return value carries no control signal — callers MUST NOT block a PR on a
// non-empty result. Blocking, when configured, is expressed solely through the
// "failure" commit status the gate posts, so branch protection (not autopilot's
// merge path) is what holds the PR. This keeps the merge flow oblivious to
// guardrails and guarantees fail-open behaviour.
//
// Every failure path returns (nil, nil): a disabled gate, an unreadable PR file
// list, or a GitHub posting error must never surface as an error that could
// abort or fail the surrounding autopilot tick.
func (g *GuardrailsGate) Evaluate(ctx context.Context, prNumber int, headSHA string) ([]architect.Violation, error) {
	if !g.Enabled() {
		return nil, nil
	}
	if headSHA == "" {
		g.log.Debug("guardrails: skipping PR with empty head SHA", "pr", prNumber)
		return nil, nil
	}

	changed, err := g.changedFiles(ctx, prNumber)
	if err != nil {
		// Fail-open: cannot read the PR's files, so we cannot judge it. Do not
		// post anything (a green status we didn't earn would be misleading; a red
		// one would block on our own outage).
		g.log.Warn("guardrails: cannot list PR files, skipping", "pr", prNumber, "error", err)
		return nil, nil
	}

	violations := g.registry.Evaluate(ctx, changed, g.worktreePath, g.cfg.DisabledRules)

	g.postStatus(ctx, headSHA, violations)
	g.postComment(ctx, prNumber, violations)
	return violations, nil
}

// changedFiles fetches the PR's changed files and reduces them to the
// project-relative path slice the rules expect.
func (g *GuardrailsGate) changedFiles(ctx context.Context, prNumber int) ([]string, error) {
	files, err := g.gh.ListPullRequestFiles(ctx, g.owner, g.repo, prNumber)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f == nil || f.Filename == "" {
			continue
		}
		out = append(out, f.Filename)
	}
	return out, nil
}

// blocking reports whether THIS run should post a failure status. Only an
// enabled "block"-mode config with at least one violation blocks; everything
// else stays green.
func (g *GuardrailsGate) blocking(violations []architect.Violation) bool {
	return g.cfg.blockingMode() && len(violations) > 0
}

// postStatus posts the commit status. Report mode (the default) is always
// success; block mode is failure only when there are violations. A posting
// error is logged and swallowed — never returned — to preserve fail-open.
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

// postComment posts the findings comment, but only when there is something to
// report. A clean PR gets no comment (just the green status), so guardrails stay
// quiet on the common case.
func (g *GuardrailsGate) postComment(ctx context.Context, prNumber int, violations []architect.Violation) {
	if len(violations) == 0 {
		return
	}
	body := renderGuardrailsComment(violations, g.cfg.EffectiveMode())
	if _, err := g.gh.AddPRComment(ctx, g.owner, g.repo, prNumber, body); err != nil {
		g.log.Warn("guardrails: failed to post PR comment", "pr", prNumber, "error", err)
	}
}

// statusDescription is the short (<=140 char) commit-status line.
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
