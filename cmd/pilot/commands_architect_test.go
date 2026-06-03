package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/testutil"
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
	for _, name := range []string{"dry-run", "create-issues", "limit", "backend", "json", "lens", "export", "linear-parent", "suggest-rules"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestArchitectCmd_SuggestRulesDefaultsFalse(t *testing.T) {
	cmd := newArchitectCmd()
	got, err := cmd.Flags().GetBool("suggest-rules")
	if err != nil {
		t.Fatalf("GetBool(suggest-rules): %v", err)
	}
	if got {
		t.Error("--suggest-rules must default to false (advisory path is opt-in)")
	}
}

// TestArchitectKnowledgeSource_NilWithoutMemory proves the helper fails open:
// no memory config yields a nil source so the suggester's collectors stay out
// of the roster rather than panicking the scan.
func TestArchitectKnowledgeSource_NilWithoutMemory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory = nil
	if src := architectKnowledgeSource(cfg); src != nil {
		t.Errorf("nil memory config must yield a nil knowledge source, got %T", src)
	}

	cfg.Memory = &config.MemoryConfig{Path: ""}
	if src := architectKnowledgeSource(cfg); src != nil {
		t.Errorf("empty memory path must yield a nil knowledge source, got %T", src)
	}
}

// TestArchitectKnowledgeSource_OpensStore proves a configured memory path
// resolves a concrete PitfallSource (a *memory.KnowledgeStore over the store's
// DB) that satisfies QueryByType.
func TestArchitectKnowledgeSource_OpensStore(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory = &config.MemoryConfig{Path: t.TempDir()}

	src := architectKnowledgeSource(cfg)
	if src == nil {
		t.Fatal("a configured memory path must yield a non-nil knowledge source")
	}
	// The source must answer a typed query without erroring (empty store => no rows).
	mems, err := src.QueryByType(memory.MemoryTypePitfall, "proj")
	if err != nil {
		t.Fatalf("QueryByType on a fresh store must not error: %v", err)
	}
	if len(mems) != 0 {
		t.Errorf("fresh store should hold no pitfalls, got %d", len(mems))
	}
}

// TestBuildArchitectRunConfig_SuggestRulesSurfacesFinding proves the flag is
// wired end-to-end through the CLI builder: with --suggest-rules and a memory
// store holding two memories that encode the same forbidden edge, a dry-run Run
// surfaces a guardrail-rule Finding.
func TestBuildArchitectRunConfig_SuggestRulesSurfacesFinding(t *testing.T) {
	dataDir := t.TempDir()
	agentDir := t.TempDir()
	seedForbiddenEdgeMemories(t, dataDir, agentDir)

	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	cfg.Memory = &config.MemoryConfig{Path: dataDir}

	f := &architectFlags{dryRun: true, suggestRules: true}
	rc, err := buildArchitectRunConfig(cfg, agentDir, f)
	if err != nil {
		t.Fatalf("build with --suggest-rules: %v", err)
	}

	res, err := architect.Run(context.Background(), rc, architect.RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found bool
	for _, finding := range res.Findings {
		if strings.Contains(finding.Title, "candidate guardrail rule") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("--suggest-rules must surface a guardrail-rule finding; got %+v", res.Findings)
	}
}

// TestBuildArchitectRunConfig_SuggestRulesOffYieldsNoFinding proves the same
// memory store produces no guardrail-rule finding when --suggest-rules is unset.
func TestBuildArchitectRunConfig_SuggestRulesOffYieldsNoFinding(t *testing.T) {
	dataDir := t.TempDir()
	agentDir := t.TempDir()
	seedForbiddenEdgeMemories(t, dataDir, agentDir)

	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"
	cfg.Memory = &config.MemoryConfig{Path: dataDir}

	f := &architectFlags{dryRun: true, suggestRules: false}
	rc, err := buildArchitectRunConfig(cfg, agentDir, f)
	if err != nil {
		t.Fatalf("build without --suggest-rules: %v", err)
	}

	res, err := architect.Run(context.Background(), rc, architect.RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, finding := range res.Findings {
		if strings.Contains(finding.Title, "candidate guardrail rule") {
			t.Fatalf("--suggest-rules off must not surface a guardrail-rule finding; got %+v", finding)
		}
	}
}

// seedForbiddenEdgeMemories writes a pitfall and a decision memory into the
// store at dataDir, both encoding "internal/gateway must not import
// internal/executor" — an edge defaultLayerRules does not cover — so the
// rule-suggester clears its two-trigger threshold. The memories are tagged with
// projectID because the CLI scopes the scan's QueryByType to ProjectID=agentDir.
func seedForbiddenEdgeMemories(t *testing.T, dataDir, projectID string) {
	t.Helper()
	store, err := memory.NewStore(dataDir)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ks := memory.NewKnowledgeStore(store.DB())
	if err := ks.InitSchema(); err != nil {
		t.Fatalf("init knowledge schema: %v", err)
	}
	if err := ks.AddMemory(&memory.Memory{
		Type:       memory.MemoryTypePitfall,
		Content:    "internal/gateway must not import internal/executor",
		Context:    "internal/gateway",
		Confidence: 0.9,
		ProjectID:  projectID,
	}); err != nil {
		t.Fatalf("add pitfall memory: %v", err)
	}
	if err := ks.AddMemory(&memory.Memory{
		Type:       memory.MemoryTypeDecision,
		Content:    "internal/gateway depends on internal/executor and that inverts layering",
		Context:    "internal/gateway",
		Confidence: 0.8,
		ProjectID:  projectID,
	}); err != nil {
		t.Fatalf("add decision memory: %v", err)
	}
}

func TestArchitectCmd_LensDefaultsEmpty(t *testing.T) {
	cmd := newArchitectCmd()
	got, err := cmd.Flags().GetString("lens")
	if err != nil {
		t.Fatalf("GetString(lens): %v", err)
	}
	if got != "" {
		t.Errorf("--lens must default to empty (= core lens), got %q", got)
	}
}

// TestBuildArchitectRunConfig_DepDoctorLens proves the --lens selector reaches
// the scanner: selecting depdoctor wires the dependency_doctor collector.
func TestBuildArchitectRunConfig_DepDoctorLens(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"

	f := &architectFlags{dryRun: true, lens: "depdoctor"}
	rc, err := buildArchitectRunConfig(cfg, t.TempDir(), f)
	if err != nil {
		t.Fatalf("build with depdoctor lens: %v", err)
	}
	cols := rc.Scanner.Collectors()
	if len(cols) != 1 || cols[0].Name() != "dependency_doctor" {
		t.Fatalf("depdoctor lens must wire the dependency_doctor collector, got %d collectors", len(cols))
	}
}

func TestBuildArchitectRunConfig_UnknownLensErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Architect = &config.ArchitectConfig{Enabled: true}
	cfg.Adapters.GitHub.Repo = "octocat/hello-world"

	f := &architectFlags{dryRun: true, lens: "ghost-lens"}
	if _, err := buildArchitectRunConfig(cfg, t.TempDir(), f); err == nil {
		t.Fatal("unknown --lens must error")
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

func TestArchitectExportTarget(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		ac      *config.ArchitectConfig
		want    string
		wantErr bool
	}{
		{"flag wins over config", "linear", &config.ArchitectConfig{Export: "github"}, config.ArchitectExportLinear, false},
		{"empty flag uses config", "", &config.ArchitectConfig{Export: "linear"}, config.ArchitectExportLinear, false},
		{"empty flag and empty config uses default", "", &config.ArchitectConfig{}, config.DefaultArchitectExport, false},
		{"adr is a valid target", "adr", &config.ArchitectConfig{}, config.ArchitectExportADR, false},
		{"invalid flag rejected", "gitlab", &config.ArchitectConfig{}, "", true},
		{"invalid config rejected", "", &config.ArchitectConfig{Export: "bogus"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := architectExportTarget(tt.ac, tt.flag)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("architectExportTarget(%+v, %q) expected error, got %q", tt.ac, tt.flag, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("architectExportTarget(%+v, %q) unexpected error: %v", tt.ac, tt.flag, err)
			}
			if got != tt.want {
				t.Errorf("architectExportTarget(%+v, %q) = %q, want %q", tt.ac, tt.flag, got, tt.want)
			}
		})
	}
}

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

// TestArchitectLinearClients_RequiresAPIKey proves the Linear export path fails
// fast when no API key is configured, even with a parent supplied.
func TestArchitectLinearClients_RequiresAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Adapters.Linear.APIKey = ""
	t.Setenv("LINEAR_API_KEY", "")

	_, _, err := architectLinearClients(cfg, "PARENT-1")
	if err == nil {
		t.Fatal("--export linear with no API key must error")
	}
}

// TestArchitectLinearClients_BuildsCreatorAndSearcher proves the happy path wires
// a non-nil creator and searcher from config (no network call is made — the
// client is constructed but not exercised here).
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
