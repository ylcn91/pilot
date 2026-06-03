package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
//
// GetPullRequest + ListIssueComments are read-only and serve two purposes:
// harvesting pilot-guardrail-allow exception directives (from the PR body and
// any comment) and locating a prior guardrails comment to update in place.
// UpdateIssueComment edits that prior comment so repeat runs never stack
// duplicates; AddPRComment creates the first one.
type guardrailsGitHub interface {
	ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]*github.PRFile, error)
	CreateCommitStatus(ctx context.Context, owner, repo, sha string, status *github.CommitStatus) (*github.CommitStatus, error)
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]*github.Comment, error)
	AddPRComment(ctx context.Context, owner, repo string, number int, body string) (*github.PRComment, error)
	UpdateIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (*github.Comment, error)
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

// Mode returns the gate's effective mode ("report" or "block"), resolving an
// empty Mode to the report-only default. A nil gate reports "report". It exists
// so the composition root can verify the mode it translated from config.
func (g *GuardrailsGate) Mode() string {
	if g == nil {
		return guardrailsModeReport
	}
	return g.cfg.EffectiveMode()
}

// Evaluate runs the guardrail rules over the PR's changed files and posts a
// commit status (always) and a PR comment (only when there is something to
// report). It honours pilot-guardrail-allow exception directives (read from the
// PR body and comments) and updates a prior guardrails comment in place rather
// than stacking duplicates.
//
// It returns the ENFORCED violations (those not waived by an exception) purely
// for caller observability/tests; the return value carries no control signal —
// callers MUST NOT block a PR on a non-empty result. Blocking, when configured,
// is expressed solely through the "failure" commit status the gate posts, so
// branch protection (not autopilot's merge path) is what holds the PR. This
// keeps the merge flow oblivious to guardrails and guarantees fail-open
// behaviour.
//
// Every failure path returns (nil, nil): a disabled gate, an unreadable PR file
// list, or a GitHub posting error must never surface as an error that could
// abort or fail the surrounding autopilot tick.
func (g *GuardrailsGate) Evaluate(ctx context.Context, prNumber int, headSHA string) ([]architect.Violation, error) {
	return g.EvaluateWithFiles(ctx, prNumber, headSHA, nil)
}

// EvaluateWithFiles is Evaluate with the PR's changed files supplied by the
// caller, so a caller that already fetched them (e.g. handleCIPassed's size
// gate) does not pay for a second ListPullRequestFiles round-trip. Pass nil to
// have the gate fetch them itself. All the fail-open guarantees of Evaluate
// hold.
func (g *GuardrailsGate) EvaluateWithFiles(ctx context.Context, prNumber int, headSHA string, files []*github.PRFile) ([]architect.Violation, error) {
	if !g.Enabled() {
		return nil, nil
	}
	if headSHA == "" {
		g.log.Debug("guardrails: skipping PR with empty head SHA", "pr", prNumber)
		return nil, nil
	}

	var changed []string
	if files != nil {
		changed = filterChangedFiles(files)
	} else {
		var err error
		changed, err = g.changedFiles(ctx, prNumber)
		if err != nil {
			// Fail-open: cannot read the PR's files, so we cannot judge it. Do not
			// post anything (a green status we didn't earn would be misleading; a
			// red one would block on our own outage).
			g.log.Warn("guardrails: cannot list PR files, skipping", "pr", prNumber, "error", err)
			return nil, nil
		}
	}

	all := g.registry.Evaluate(ctx, changed, g.worktreePath, g.cfg.DisabledRules)

	// Harvest exception directives from the PR body + comments, then split the
	// findings. Reading exceptions is best-effort: a fetch error simply means no
	// exceptions are honoured (fail-closed for exceptions, fail-open for the
	// gate), never an abort.
	allowed, priorComment := g.scanPR(ctx, prNumber)
	enforced, excepted, usedExceptions := partitionViolations(all, allowed)

	g.postStatus(ctx, headSHA, enforced)
	g.postComment(ctx, prNumber, enforced, excepted, usedExceptions, priorComment)
	return enforced, nil
}

// scanPR fetches the PR body and every comment, returning the union of allowed
// rule names (from pilot-guardrail-allow directives) and the prior guardrails
// comment, if any, so it can be updated in place. Each fetch is independent and
// best-effort: a failure on one does not abort the other or the gate.
func (g *GuardrailsGate) scanPR(ctx context.Context, prNumber int) (allowed map[string]bool, prior *github.Comment) {
	allowed = make(map[string]bool)

	if pr, err := g.gh.GetPullRequest(ctx, g.owner, g.repo, prNumber); err != nil {
		g.log.Warn("guardrails: cannot fetch PR body for exceptions", "pr", prNumber, "error", err)
	} else if pr != nil {
		mergeAllowed(allowed, parseAllowedRules(pr.Body))
	}

	comments, err := g.gh.ListIssueComments(ctx, g.owner, g.repo, prNumber)
	if err != nil {
		g.log.Warn("guardrails: cannot list PR comments (exceptions + dedup)", "pr", prNumber, "error", err)
		return allowed, nil
	}
	for _, cm := range comments {
		if cm == nil {
			continue
		}
		mergeAllowed(allowed, parseAllowedRules(cm.Body))
		if prior == nil && strings.Contains(cm.Body, guardrailsCommentMarker) {
			prior = cm
		}
	}
	return allowed, prior
}

// mergeAllowed folds src into dst.
func mergeAllowed(dst, src map[string]bool) {
	for k := range src {
		dst[k] = true
	}
}

// changedFiles fetches the PR's changed files and reduces them to the
// project-relative path slice the rules expect.
func (g *GuardrailsGate) changedFiles(ctx context.Context, prNumber int) ([]string, error) {
	files, err := g.gh.ListPullRequestFiles(ctx, g.owner, g.repo, prNumber)
	if err != nil {
		return nil, err
	}
	return filterChangedFiles(files), nil
}

// filterChangedFiles reduces a PR file list to the non-empty, project-relative
// filename slice the rules consume, dropping nil entries and empty names.
func filterChangedFiles(files []*github.PRFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f == nil || f.Filename == "" {
			continue
		}
		out = append(out, f.Filename)
	}
	return out
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

// postComment posts (or updates) the findings comment. A comment is warranted
// when there is anything to surface: an enforced violation, or an acknowledged
// exception worth recording. A fully clean PR gets no comment (just the green
// status), so guardrails stay quiet on the common case.
//
// Dedup: if a prior guardrails comment exists (located by marker during
// scanPR), its body is edited in place via UpdateIssueComment; otherwise a new
// comment is created. This keeps exactly one guardrails comment per PR across
// repeated runs.
func (g *GuardrailsGate) postComment(ctx context.Context, prNumber int, enforced, excepted []architect.Violation, usedExceptions []string, prior *github.Comment) {
	if len(enforced) == 0 && len(excepted) == 0 {
		// Nothing to report. If a prior comment from an earlier (dirty) run is
		// lingering, leave it untouched: editing it to "all clear" is out of
		// scope, and removing it risks deleting a human's reply thread anchor.
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

// runGuardrailsGate runs the optional per-PR architectural guardrails gate as a
// FAIL-OPEN side-effect of handleCIPassed. It NEVER changes prState.Stage or
// otherwise influences the merge decision: blocking, when configured, is
// surfaced purely through the gate's pilot/guardrails commit status, leaving the
// autopilot merge path oblivious to guardrails. A nil/disabled gate is a no-op.
//
// files are the PR's changed files already fetched by handleCIPassed; pass nil
// to let the gate fetch them itself. Any error — or even a panic inside a buggy
// rule — is contained here so guardrails can never break an existing autopilot
// flow.
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
		// EvaluateWithFiles is already fail-open and returns nil error, but guard
		// anyway so a future change cannot leak an error into the merge path.
		c.log.Warn("guardrails gate errored, continuing (fail-open)",
			"pr", prState.PRNumber, "error", err)
		return
	}
	if len(violations) > 0 {
		c.log.Info("guardrails gate found violations (report-only unless block mode)",
			"pr", prState.PRNumber, "count", len(violations))
	}
}
