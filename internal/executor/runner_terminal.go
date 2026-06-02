package executor

import "strings"

// permanentFailurePatterns are substrings in error messages that indicate
// failures which won't change between retries (e.g. invalid issue title).
// GH-2402: terminal-classify these so the poller stops the retry loop.
var permanentFailurePatterns = []string{
	"PR creation refused",
	// GH-3224: a no-op run (ghost-SHA guard tripped — both the worktree-HEAD and
	// post-push variants begin with this prefix) is deterministic: retrying the
	// identical prompt reproduces it (observed 4× on GH-3228). Classify terminal
	// so the poller labels pilot-blocked instead of burning cycles on pilot-failed
	// retries. A human (or a re-dispatch carrying the EvidenceBackedSpecDirective)
	// resolves it.
	"no new commit produced",
}

// IsPermanentFailure reports whether an error message represents a
// deterministic failure that won't change between retries. Callers should
// label such failures with pilot-blocked instead of pilot-failed so the
// poller doesn't burn cycles on identical retries. GH-2402.
func IsPermanentFailure(errStr string) bool {
	if errStr == "" {
		return false
	}
	for _, pat := range permanentFailurePatterns {
		if strings.Contains(errStr, pat) {
			return true
		}
	}
	return false
}

// Non-failure terminal-outcome signatures. The dispatcher classifies an execution
// by the deterministic fragments the runner (or its subprocess) writes to
// result.Error, so the dashboard's "failed" count reflects genuine task failures
// only. Matching is case-insensitive (see containsAny). Evaluation order in
// outcomeClassifiers is significant. TASK-358.
var (
	// noOp: the agent produced no code change for a non-failure reason — the work
	// was already on the base branch (TASK-321 phantom no-op) or it made no edits.
	noOpErrorSignatures = []string{
		"no new commit produced",      // ghost-SHA guard: HEAD/post-push SHA matches base
		"no commits relative to base", // PR guard: empty branch
		"no_changes",                  // tagged no-change run
		"made no code changes",        // legacy no-change message (lacks the "no_changes:" prefix)
	}
	// rateLimited: provider/model quota was hit — transient, not a failure.
	rateLimitedSignatures = []string{
		"hit your limit",
		"rate limit",
		"usage limit",
	}
	// skipped: the task never really executed — no worker picked it up, or the run
	// was cancelled (shutdown / context canceled).
	skippedSignatures = []string{
		"stale queued task recovered",
		"context canceled",
		"context cancelled",
	}
	// stalled: an incomplete run — watchdog stall or per-task budget cap.
	stalledErrorSignatures = []string{
		"session stalled",
		"budget limit exceeded",
	}
	// infra: operational/plumbing failure — the agent's work may be fine but Pilot
	// could not run or land it (resource kill, push/PR/worktree/branch failure).
	infraErrorSignatures = []string{
		"oom_killed",
		"exit code 137",
		"sigkill",
		"signal: killed",
		"push failed",
		"pr creation failed", // distinct from "PR creation refused" (a genuine title-guard failure)
		"worktree creation failed",
		"create/switch branch",
		"branch switch failed",
	}
)

// outcomeClassifiers is the ordered fallback table used when result.Outcome was
// not set explicitly (older rows, or terminal paths that pre-date Outcome). First
// match wins, so the most "this isn't a failure" signal (no-op) is checked before
// the most failure-like (infra). TASK-358.
var outcomeClassifiers = []struct {
	status     string
	signatures []string
}{
	{"no_op", noOpErrorSignatures},
	{"rate_limited", rateLimitedSignatures},
	{"skipped", skippedSignatures},
	{"stalled", stalledErrorSignatures},
	{"infra", infraErrorSignatures},
}

// TerminalStatus maps a finished ExecutionResult to the status persisted in the
// executions table so the dashboard's "failed" count reflects genuine task
// failures only. Non-failure outcomes (no-op / rate-limited / skipped / stalled /
// infra / declined) get their own status instead of collapsing into "failed",
// which historically inflated the QUEUE card. TASK-358.
//
// Precedence: Success → Declined → explicit Outcome tag → ordered error-signature
// table → "failed". A genuine failure (quality gates, planning, unknown exit) has
// none of the non-failure signals and correctly falls through to "failed".
func TerminalStatus(result *ExecutionResult) string {
	if result == nil {
		return "failed"
	}
	if result.Success {
		return "completed"
	}
	if result.Declined {
		return "declined"
	}
	switch result.Outcome {
	case "declined":
		return "declined"
	case "no_op", "no_commits":
		return "no_op"
	case "stalled", "budget_exceeded":
		return "stalled"
	case "rate_limited":
		return "rate_limited"
	case "infra":
		return "infra"
	case "skipped":
		return "skipped"
	}
	for _, c := range outcomeClassifiers {
		if containsAny(result.Error, c.signatures) {
			return c.status
		}
	}
	return "failed"
}
