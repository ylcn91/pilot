package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// conventionalCommitRE matches conventional commit titles per the spec used by Pilot.
var conventionalCommitRE = regexp.MustCompile(`^(feat|fix|chore|refactor|test|docs|perf|build|ci|style)(\([^)]+\))?: .+$`)

// envBypassIssueAllowlist is the env var that bypasses the repo allowlist check at
// the CreatePilotIssue level. Must match executor.envBypassRepoAllowlist.
// The protected upstream repo is never bypassable.
const envBypassIssueAllowlist = "PILOT_ALLOW_UNMANAGED_REPO"

const (
	protectedUpstreamOwner = "qf-studio"
	protectedUpstreamRepo  = "pilot"
)

// envDisableIssueCreation is the global kill-switch that disables ALL GitHub
// issue creation. Added after the GH-201 OAuth-cascade incident, where a daemon
// pointed at the upstream repo re-dispatched a closed parent and spawned hundreds
// of hallucinated "feat(auth): add OAuth provider integration" sub-issues. When
// set truthy ("1"/"true"/"yes"/"on") every creation chokepoint refuses to create.
const envDisableIssueCreation = "PILOT_DISABLE_ISSUE_CREATION"

// ErrIssueCreationDisabled is returned by the creation chokepoints when the
// kill-switch is engaged. Callers should treat it as a clean skip, not a failure.
var ErrIssueCreationDisabled = errors.New("github issue creation disabled (PILOT_DISABLE_ISSUE_CREATION)")

// IssueCreationDisabled reports whether the issue-creation kill-switch is engaged.
func IssueCreationDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envDisableIssueCreation))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// IssueAllowlist is the minimal surface CreatePilotIssue needs to validate that the
// target (owner, repo) is in the user's configured project list. executor.RepoAllowlist
// satisfies this interface — callers can pass it directly. When nil, the check fails
// closed unless PILOT_ALLOW_UNMANAGED_REPO=1 is set.
//
// This interface is defined here (rather than importing executor.RepoAllowlist) to avoid
// an import cycle: internal/config imports internal/adapters/github, so neither executor
// nor config can be imported from this package.
type IssueAllowlist interface {
	// RepoIsAllowed reports whether (owner, repo) is a configured Pilot project.
	RepoIsAllowed(owner, repo, projectPath string) bool
	// ConfiguredRepos returns "owner/repo" strings for error messages only.
	ConfiguredRepos() []string
}

// allowAllIssueRepos is an IssueAllowlist that permits any (owner, repo). Use it
// at call sites where owner/repo are already constrained by explicit config (e.g.
// the autopilot feedback loop) so the intent is encoded at the call site rather
// than relying on a permissive nil default. TASK-347.
type allowAllIssueRepos struct{}

func (allowAllIssueRepos) RepoIsAllowed(string, string, string) bool { return true }
func (allowAllIssueRepos) ConfiguredRepos() []string                 { return []string{"*"} }

// AllowAllIssueRepos returns an IssueAllowlist that permits any repo, for callers
// whose target is already constrained by their own configuration. TASK-347.
func AllowAllIssueRepos() IssueAllowlist { return allowAllIssueRepos{} }

// CreatePilotIssue validates the title against the conventional-commits format and
// creates a GitHub issue with the given labels.
//
// allow is the repo allowlist for TASK-286 / GH-3027 defense-in-depth. Pass a non-nil
// IssueAllowlist to enforce that (owner, repo) is a configured Pilot project. A nil
// allowlist now FAILS CLOSED (TASK-347) to match executor.ValidateTargetRepo — for a
// caller whose owner/repo is already constrained by its own config, pass
// AllowAllIssueRepos() to encode that intent. Set PILOT_ALLOW_UNMANAGED_REPO=1 to bypass
// (covers both the nil case and a non-nil allowlist that would otherwise reject the repo).
//
// Returns an error if the title does not match conventional-commits format, if the
// allowlist rejects the repo, or if the GitHub API call fails.
func CreatePilotIssue(ctx context.Context, c *Client, allow IssueAllowlist, owner, repo, title, body string, labels []string) (*Issue, error) {
	if IssueCreationDisabled() {
		slog.Warn("CreatePilotIssue skipped: issue creation disabled",
			"component", "adapters.github.issue_create",
			"owner", owner,
			"repo", repo,
			"title", title,
		)
		return nil, ErrIssueCreationDisabled
	}
	if err := validateIssueRepo(allow, owner, repo); err != nil {
		return nil, fmt.Errorf("CreatePilotIssue repo guardrail: %w", err)
	}
	if !conventionalCommitRE.MatchString(title) {
		return nil, fmt.Errorf("issue title %q does not match conventional-commits format (type(scope): description)", title)
	}
	return c.CreateIssue(ctx, owner, repo, &IssueInput{
		Title:  title,
		Body:   body,
		Labels: labels,
	})
}

// validateIssueRepo enforces the repo allowlist. Mirrors executor.ValidateTargetRepo
// but is defined here to avoid the executor→github import cycle.
func validateIssueRepo(allow IssueAllowlist, owner, repo string) error {
	bypass := os.Getenv(envBypassIssueAllowlist) == "1"

	if isProtectedUpstreamRepo(owner, repo) {
		return fmt.Errorf("refusing to create issues on protected upstream %s/%s", owner, repo)
	}

	if allow == nil {
		// C7 (TASK-347): fail closed to match executor.ValidateTargetRepo — a future
		// caller that forgets to wire an allowlist must not silently get zero
		// enforcement (the GH-3027 cross-repo-leak class). Known-safe callers pass
		// AllowAllIssueRepos() to encode "intentionally unrestricted" explicitly.
		if bypass {
			slog.Warn("CreatePilotIssue: no IssueAllowlist configured; PILOT_ALLOW_UNMANAGED_REPO=1 bypassed the repo check",
				"component", "adapters.github.issue_create",
				"owner", owner,
				"repo", repo,
			)
			return nil
		}
		return fmt.Errorf("no IssueAllowlist configured for %s/%s — pass an allowlist (or AllowAllIssueRepos()) or set %s=1",
			owner, repo, envBypassIssueAllowlist)
	}

	if allow.RepoIsAllowed(owner, repo, "") {
		return nil
	}

	if bypass {
		slog.Warn("PILOT_ALLOW_UNMANAGED_REPO=1 bypassed IssueAllowlist",
			"component", "adapters.github.issue_create",
			"owner", owner,
			"repo", repo,
			"configured_repos", strings.Join(allow.ConfiguredRepos(), ","),
		)
		return nil
	}

	return fmt.Errorf("%s/%s not in configured projects [%s]",
		owner, repo, strings.Join(allow.ConfiguredRepos(), ","))
}

func isProtectedUpstreamRepo(owner, repo string) bool {
	return strings.EqualFold(owner, protectedUpstreamOwner) && strings.EqualFold(repo, protectedUpstreamRepo)
}
