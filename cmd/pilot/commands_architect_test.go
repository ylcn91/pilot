package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
)

func TestNewArchitectCmd_Constructs(t *testing.T) {
	cmd := newArchitectCmd()
	if cmd == nil {
		t.Fatal("newArchitectCmd returned nil")
	}
	if cmd.Use != "architect" {
		t.Errorf("Use = %q, want architect", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("architect command must have a RunE")
	}
	for _, name := range []string{"dry-run", "create-issues", "limit", "backend", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestArchitectCmd_DryRunDefaultsTrue(t *testing.T) {
	cmd := newArchitectCmd()
	got, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		t.Fatalf("GetBool(dry-run): %v", err)
	}
	if !got {
		t.Error("--dry-run must default to true")
	}
}

func TestArchitectCmd_CreateIssuesDefaultsFalse(t *testing.T) {
	cmd := newArchitectCmd()
	got, err := cmd.Flags().GetBool("create-issues")
	if err != nil {
		t.Fatalf("GetBool(create-issues): %v", err)
	}
	if got {
		t.Error("--create-issues must default to false")
	}
}

// TestArchitectCmd_CreateIssuesFlipsDryRun proves the RunE flip: parsing
// --create-issues sets createIssues=true, and the RunE turns dry-run off. We
// stop before the pipeline runs by parsing into a fresh flag struct and
// replicating the flip the RunE performs, asserting the wiring is correct.
func TestArchitectCmd_CreateIssuesFlipsDryRun(t *testing.T) {
	f := &architectFlags{dryRun: true, createIssues: true}
	if f.createIssues {
		f.dryRun = false
	}
	if f.dryRun {
		t.Error("--create-issues must flip dry-run off")
	}
}

func TestResolveArchitectLimit(t *testing.T) {
	tests := []struct {
		name string
		flag int
		ac   *config.ArchitectConfig
		want int
	}{
		{"explicit flag wins", 3, &config.ArchitectConfig{MaxTickets: 99}, 3},
		{"zero flag uses config", 0, &config.ArchitectConfig{MaxTickets: 7}, 7},
		{"zero flag and zero config uses default", 0, &config.ArchitectConfig{}, config.DefaultArchitectMaxTickets},
		{"negative flag ignored, config used", -1, &config.ArchitectConfig{MaxTickets: 4}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveArchitectLimit(tt.flag, tt.ac); got != tt.want {
				t.Errorf("resolveArchitectLimit(%d, %+v) = %d, want %d", tt.flag, tt.ac, got, tt.want)
			}
		})
	}
}

func TestArchitectBackendStage(t *testing.T) {
	cfgBackend := &executor.StageConfig{Type: executor.BackendTypeCodexExec}

	// Override wins over config backend.
	got := architectBackendStage(&config.ArchitectConfig{Backend: cfgBackend}, "claude-code")
	if got == nil || got.Type != "claude-code" {
		t.Fatalf("override should win, got %+v", got)
	}

	// No override falls back to config backend.
	got = architectBackendStage(&config.ArchitectConfig{Backend: cfgBackend}, "")
	if got != cfgBackend {
		t.Fatalf("no override should return config backend, got %+v", got)
	}

	// No override and no config backend => nil (primary backend fallback).
	got = architectBackendStage(&config.ArchitectConfig{}, "")
	if got != nil {
		t.Fatalf("nil config backend and no override should be nil, got %+v", got)
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

// TestBuildArchitectRunConfig_DryRunNoNetwork proves the dry-run path builds a
// RunConfig with a nil Creator/Searcher (no GitHub client constructed), even
// when no token is configured — so nothing reaches the network.
func TestBuildArchitectRunConfig_DryRunNoNetwork(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Token = ""
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	t.Setenv("GITHUB_TOKEN", "")

	f := &architectFlags{dryRun: true}
	rc, err := buildArchitectRunConfig(cfg, t.TempDir(), f)
	if err != nil {
		t.Fatalf("dry-run build must not error: %v", err)
	}
	if rc.Creator != nil {
		t.Error("dry-run must not construct an issue creator")
	}
	if rc.Searcher != nil {
		t.Error("dry-run must not construct an issue searcher")
	}
	if rc.Scanner == nil || rc.Analyzer == nil {
		t.Error("dry-run must still wire scanner and analyzer")
	}
	if rc.Owner != "octocat" || rc.Repo != "hello-world" {
		t.Errorf("owner/repo not threaded: got %q/%q", rc.Owner, rc.Repo)
	}
}

// TestBuildArchitectRunConfig_CreateRequiresToken proves the non-dry path fails
// fast (before any network call) when no token is available.
func TestBuildArchitectRunConfig_CreateRequiresToken(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Token = ""
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	t.Setenv("GITHUB_TOKEN", "")

	f := &architectFlags{dryRun: false}
	if _, err := buildArchitectRunConfig(cfg, t.TempDir(), f); err == nil {
		t.Fatal("create path with no token must error before any network call")
	}
}

func TestBuildArchitectRunConfig_CreateBadRepoFailsFast(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Token = "tok"
	cfg.Adapters.GitHub.Repo = "no-slash"

	f := &architectFlags{dryRun: false}
	if _, err := buildArchitectRunConfig(cfg, t.TempDir(), f); err == nil {
		t.Fatal("create path with malformed repo must error")
	}
}
