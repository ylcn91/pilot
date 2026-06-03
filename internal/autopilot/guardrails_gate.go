package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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

// materializeHead resolves the content source the rules read for this run.
//
// When repoPath is a git work tree it fetches the PR head SHA and writes each
// changed file's content at that SHA into a fresh tempdir, returning that dir so
// the rules judge the PR's post-change content (net-new files now exist, changed
// files reflect the PR, deleted files are correctly absent). cleanup removes the
// tempdir; it is always safe to call.
//
// When repoPath is empty or not a git work tree it is returned as-is with a
// no-op cleanup: an empty path degrades the rules to no findings (fail-open) and
// a plain directory is used directly as the content source (fixture-backed
// tests). ok is false only on a hard materialization failure (fetch/show/mkdir),
// which is the gate's fail-open skip: the caller posts nothing.
func (g *GuardrailsGate) materializeHead(ctx context.Context, headSHA string, changed []string) (worktreePath string, cleanup func(), ok bool) {
	noop := func() {}
	if g.repoPath == "" || !isGitWorkTree(g.repoPath) {
		return g.repoPath, noop, true
	}

	if err := g.ensureHead(ctx, headSHA); err != nil {
		g.log.Warn("guardrails: cannot resolve PR head, skipping", "sha", ShortSHA(headSHA), "error", err)
		return "", noop, false
	}

	tmp, err := os.MkdirTemp("", "pilot-guardrails-*")
	if err != nil {
		g.log.Warn("guardrails: cannot create temp worktree, skipping", "error", err)
		return "", noop, false
	}
	cleanup = func() {
		if rmErr := os.RemoveAll(tmp); rmErr != nil {
			g.log.Warn("guardrails: failed to remove temp worktree", "dir", tmp, "error", rmErr)
		}
	}

	for _, rel := range changed {
		if err := ctx.Err(); err != nil {
			cleanup()
			g.log.Warn("guardrails: context cancelled while materializing head, skipping", "error", err)
			return "", noop, false
		}
		// A file deleted in the PR does not exist at the head SHA: git show fails,
		// so we simply skip writing it. The rules then correctly see it as absent.
		content, showErr := g.showHeadFile(ctx, headSHA, rel)
		if showErr != nil {
			continue
		}
		if writeErr := writeMaterializedFile(tmp, rel, content); writeErr != nil {
			cleanup()
			g.log.Warn("guardrails: cannot write materialized file, skipping", "file", rel, "error", writeErr)
			return "", noop, false
		}
	}
	return tmp, cleanup, true
}

// ensureHead makes the head SHA's commit object available in repoPath so a
// subsequent git show can read its content. The clone may not have seen the PR
// head yet, so it fetches from origin — but the fetch is best-effort: if the
// object is already present (the common case once the clone is up to date, and
// always true for a local-only test repo with no origin), a fetch failure is
// irrelevant. It only errors when the object is still unresolvable after the
// fetch attempt, which is the gate's fail-open skip.
func (g *GuardrailsGate) ensureHead(ctx context.Context, headSHA string) error {
	if g.headPresent(ctx, headSHA) {
		return nil
	}
	fetch := exec.CommandContext(ctx, "git", "-C", g.repoPath, "fetch", "--quiet", "origin", headSHA)
	fetchOut, fetchErr := fetch.CombinedOutput()
	if g.headPresent(ctx, headSHA) {
		return nil
	}
	if fetchErr != nil {
		return fmt.Errorf("git fetch %s: %w: %s", ShortSHA(headSHA), fetchErr, strings.TrimSpace(string(fetchOut)))
	}
	return fmt.Errorf("commit %s not present after fetch", ShortSHA(headSHA))
}

// headPresent reports whether the head SHA's commit object exists locally.
func (g *GuardrailsGate) headPresent(ctx context.Context, headSHA string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", g.repoPath, "cat-file", "-e", headSHA+"^{commit}")
	return cmd.Run() == nil
}

// showHeadFile returns the content of a project-relative file at the head SHA,
// or an error when the file does not exist there (e.g. deleted in the PR).
func (g *GuardrailsGate) showHeadFile(ctx context.Context, headSHA, rel string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", g.repoPath, "show", headSHA+":"+filepath.ToSlash(rel))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// writeMaterializedFile writes content to dir/rel, creating parent directories.
func writeMaterializedFile(dir, rel string, content []byte) error {
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

// isGitWorkTree reports whether dir contains a .git entry (directory for a normal
// clone, file for a linked work tree), i.e. whether the gate should materialise
// PR-head content from it rather than read it directly.
func isGitWorkTree(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
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
