package autopilot

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestResolveMainBranchName_PrefersEnvBranch asserts the happy path of the
// branch resolver: when the resolved environment carries a Branch, that name is
// returned verbatim and NO fallback WARN is logged. This is the unit-level
// counterpart to TestGetMainBranchSHA_RespectsResolvedEnv, which only observes
// the resolver indirectly through the GitHub branch endpoint. (TASK-291)
func TestResolveMainBranchName_PrefersEnvBranch(t *testing.T) {
	for _, branch := range []string{"main", "develop", "master", "trunk", "release/v2"} {
		t.Run(branch, func(t *testing.T) {
			ghClient := github.NewClient(testutil.FakeGitHubToken)
			cfg := DefaultConfig()
			cfg.activeEnvName = "test-env"
			cfg.activeEnvConfig = &EnvironmentConfig{Branch: branch}

			c := NewController(cfg, ghClient, nil, "owner", "repo")

			var buf bytes.Buffer
			c.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

			if got := c.resolveMainBranchName(); got != branch {
				t.Errorf("resolveMainBranchName() = %q, want %q", got, branch)
			}
			if strings.Contains(buf.String(), "no environment branch configured") {
				t.Errorf("must NOT log the fallback WARN when a branch is configured; log was: %q", buf.String())
			}
		})
	}
}

// TestResolveMainBranchName_FallbackToMain covers the explicit gap: when the
// resolved environment has an EMPTY Branch, resolveMainBranchName must return
// the literal "main" AND emit the WARN log instructing the operator to set
// environments.<env>.branch. The WARN path is the diagnostic signal that the
// repo is relying on the last-resort default, so the test asserts both the
// returned value and the log line. (TASK-291)
func TestResolveMainBranchName_FallbackToMain(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()
	// Empty Branch on the active env forces the fallback branch of the resolver.
	cfg.activeEnvName = "test-env"
	cfg.activeEnvConfig = &EnvironmentConfig{Branch: ""}

	c := NewController(cfg, ghClient, nil, "owner", "repo")

	var buf bytes.Buffer
	c.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	got := c.resolveMainBranchName()
	if got != "main" {
		t.Errorf("resolveMainBranchName() = %q, want literal fallback %q", got, "main")
	}

	logged := buf.String()
	if !strings.Contains(logged, "no environment branch configured") {
		t.Errorf("expected fallback WARN to be logged, got: %q", logged)
	}
	if !strings.Contains(logged, "level=WARN") {
		t.Errorf("fallback must be logged at WARN level, got: %q", logged)
	}
	// The owner/repo context is part of the diagnostic so an operator can locate
	// the misconfigured repo.
	if !strings.Contains(logged, "owner=owner") || !strings.Contains(logged, "repo=repo") {
		t.Errorf("fallback WARN must carry owner/repo context, got: %q", logged)
	}
}
