package autopilot

import (
	"context"
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
	gh       guardrailsGitHub
	registry ruleEvaluator
	cfg      GuardrailsGateConfig
	// repoPath is the local clone the gate fetches the PR head from. Per run the
	// gate materialises the PR's changed files at that head SHA into a fresh
	// tempdir and points the rules at THAT, so they judge PR content (net-new and
	// changed files included) instead of the clone's currently checked-out
	// branch. When repoPath is not a git work tree it is passed to the rules
	// directly, which keeps fixture-backed tests (and an empty repoPath, which
	// degrades the rules to no findings) working unchanged.
	repoPath string
	owner    string
	repo     string
	log      *slog.Logger
}

// NewGuardrailsGate builds a gate. repoPath is the local clone the rules read
// from: per run the gate materialises the PR head's content into a tempdir and
// evaluates the rules against that, so they see the PR's post-change files. When
// repoPath is empty the rules degrade to no findings, keeping the gate
// fail-open; when repoPath is a directory that is not a git work tree it is used
// directly as the content source (fixture-backed tests).
func NewGuardrailsGate(gh guardrailsGitHub, registry ruleEvaluator, cfg GuardrailsGateConfig, repoPath, owner, repo string) *GuardrailsGate {
	return &GuardrailsGate{
		gh:       gh,
		registry: registry,
		cfg:      cfg,
		repoPath: repoPath,
		owner:    owner,
		repo:     repo,
		log:      slog.Default().With("component", "guardrails-gate"),
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

	// Materialise the PR head's content so the rules judge the PR's post-change
	// files (net-new and modified) rather than whatever branch repoPath currently
	// has checked out. This is fail-open: a fetch/show/mkdir failure logs and
	// returns (nil, nil), exactly like every other error path, so a fetch outage
	// can never block a PR.
	worktreePath, cleanup, ok := g.materializeHead(ctx, headSHA, changed)
	if !ok {
		return nil, nil
	}
	defer cleanup()

	all := g.registry.Evaluate(ctx, changed, worktreePath, g.cfg.DisabledRules)

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
