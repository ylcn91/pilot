package main

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestArchitectLinearClients_RequiresParent proves the --export linear path fails
// fast (before any network call) when --linear-parent is empty.
func TestArchitectLinearClients_RequiresParent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.Linear.APIKey = testutil.FakeLinearAPIKey

	_, _, err := architectLinearClients(cfg, "")
	if err == nil {
		t.Fatal("--export linear with no --linear-parent must error")
	}
	if !strings.Contains(err.Error(), "linear-parent") {
		t.Errorf("error should mention linear-parent, got %q", err)
	}
}

func TestArchitectLinearClients_RequiresAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.Linear.APIKey = ""
	t.Setenv("LINEAR_API_KEY", "")

	_, _, err := architectLinearClients(cfg, "PARENT-1")
	if err == nil {
		t.Fatal("--export linear with no API key must error")
	}
}

func TestArchitectLinearClients_BuildsCreatorAndSearcher(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.Linear.APIKey = testutil.FakeLinearAPIKey

	creator, searcher, err := architectLinearClients(cfg, "PARENT-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creator == nil {
		t.Error("expected a non-nil Linear issue creator")
	}
	if searcher == nil {
		t.Error("expected a non-nil Linear issue searcher")
	}
}

func TestArchitectLinearAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.Linear.APIKey = "config-linear-key"
	if got := architectLinearAPIKey(cfg); got != "config-linear-key" {
		t.Errorf("config key should win, got %q", got)
	}

	cfg.Adapters.Linear.APIKey = ""
	t.Setenv("LINEAR_API_KEY", "env-linear-key")
	if got := architectLinearAPIKey(cfg); got != "env-linear-key" {
		t.Errorf("env key fallback failed, got %q", got)
	}
}

func TestArchitectBackendStage(t *testing.T) {
	cfgBackend := &executor.StageConfig{Type: executor.BackendTypeCodexExec}

	got := architectBackendStage(&config.ArchitectConfig{Backend: cfgBackend}, "claude-code")
	if got == nil || got.Type != "claude-code" {
		t.Fatalf("override should win, got %+v", got)
	}

	got = architectBackendStage(&config.ArchitectConfig{Backend: cfgBackend}, "")
	if got != cfgBackend {
		t.Fatalf("no override should return config backend, got %+v", got)
	}

	got = architectBackendStage(&config.ArchitectConfig{}, "")
	if got != nil {
		t.Fatalf("nil config backend and no override should be nil, got %+v", got)
	}
}

func TestValidateArchitectBackendStage(t *testing.T) {
	for _, typ := range []string{executor.BackendTypeCodexExec, executor.BackendTypeClaudeCode} {
		if err := validateArchitectBackendStage(&executor.StageConfig{Type: typ}, "--backend"); err != nil {
			t.Fatalf("%s should be valid for architect mode: %v", typ, err)
		}
	}

	err := validateArchitectBackendStage(&executor.StageConfig{Type: "codex-app-server"}, "--backend")
	if err == nil {
		t.Fatal("codex-app-server must be rejected")
	}
	for _, want := range []string{"--backend", "not a runnable Backend", "use codex-exec"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestArchitectOwnerRepo(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	owner, repo, err := architectOwnerRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "octocat" || repo != "hello-world" {
		t.Errorf("got %q/%q, want octocat/hello-world", owner, repo)
	}
}

func TestArchitectOwnerRepo_Errors(t *testing.T) {
	tests := []struct {
		name string
		repo string
	}{
		{"empty", ""},
		{"no slash", "octocat"},
		{"missing owner", "/hello-world"},
		{"missing repo", "octocat/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Adapters.GitHub.Repo = tt.repo
			if _, _, err := architectOwnerRepo(cfg); err == nil {
				t.Errorf("repo %q should error", tt.repo)
			}
		})
	}
}

func TestArchitectGitHubToken(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.GitHub.Token = "config-token"
	if got := architectGitHubToken(cfg); got != "config-token" {
		t.Errorf("config token should win, got %q", got)
	}

	cfg.Adapters.GitHub.Token = ""
	t.Setenv("GITHUB_TOKEN", "env-token")
	if got := architectGitHubToken(cfg); got != "env-token" {
		t.Errorf("env token fallback failed, got %q", got)
	}
}

func TestArchitectBaseBackend_DefaultsWhenNil(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Executor = nil
	got := architectBaseBackend(cfg)
	if got.Type == "" {
		t.Error("nil executor must fall back to a default backend with a Type")
	}
}

func TestBuildArchitectRunConfig_CodexExecBackendAccepted(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"

	f := &architectFlags{dryRun: true, backend: executor.BackendTypeCodexExec}
	if _, err := buildArchitectRunConfig(cfg, t.TempDir(), f); err != nil {
		t.Fatalf("codex-exec override must build architect run config: %v", err)
	}
}

func TestBuildArchitectRunConfig_RejectsCodexAppServerOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"

	f := &architectFlags{dryRun: true, backend: "codex-app-server"}
	_, err := buildArchitectRunConfig(cfg, t.TempDir(), f)
	if err == nil {
		t.Fatal("codex-app-server override must be rejected")
	}
	if !strings.Contains(err.Error(), "use codex-exec") {
		t.Fatalf("error should point to codex-exec, got %q", err)
	}
}
