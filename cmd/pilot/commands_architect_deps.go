package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/adapters/linear"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
)

// architectLinearClients builds the Linear issue creator (sub-issues under
// parentID) and dedup searcher. It requires a non-empty parentID — Linear's
// CreateIssue derives team/project from an existing parent — and an API key from
// config or LINEAR_API_KEY. *linear.Client satisfies both the SubIssueCreator
// and IssueSearcher seams.
func architectLinearClients(cfg *config.Config, parentID string) (architect.IssueCreator, architect.IssueSearcher, error) {
	if strings.TrimSpace(parentID) == "" {
		return nil, nil, fmt.Errorf("--linear-parent is required when --export linear; pass an existing Linear issue ID to file sub-issues under")
	}
	client := architectLinearClient(cfg)
	if client == nil {
		return nil, nil, fmt.Errorf("Linear API key not configured; set adapters.linear.api_key or LINEAR_API_KEY to export to Linear")
	}
	creator := architect.NewLinearIssueCreator(client, parentID)
	return creator, client, nil
}

// architectLinearClient resolves the Linear client from the LINEAR_API_KEY
// source (config adapters.linear.api_key, then the env var), returning nil when
// no key is available so callers can fail with an actionable error.
func architectLinearClient(cfg *config.Config) *linear.Client {
	key := architectLinearAPIKey(cfg)
	if key == "" {
		return nil
	}
	return linear.NewClient(key)
}

// architectLinearAPIKey resolves the Linear API key from config, falling back to
// the LINEAR_API_KEY environment variable.
func architectLinearAPIKey(cfg *config.Config) string {
	if cfg.Adapters != nil && cfg.Adapters.Linear != nil && cfg.Adapters.Linear.APIKey != "" {
		return cfg.Adapters.Linear.APIKey
	}
	return os.Getenv("LINEAR_API_KEY")
}

// architectBackendStage resolves the PROPOSE backend stage: an explicit
// --backend override wins, then the config's architect.backend, else nil
// (fall back to the primary executor backend).
func architectBackendStage(ac *config.ArchitectConfig, override string) *executor.StageConfig {
	if override != "" {
		return &executor.StageConfig{Type: override}
	}
	return ac.Backend
}

func architectBackendLabel(f *architectFlags) string {
	if f.backend != "" {
		return "--backend"
	}
	return "architect.backend"
}

func validateArchitectBackendStage(stage *executor.StageConfig, label string) error {
	if stage == nil {
		return nil
	}
	if stage.Type == "" {
		return fmt.Errorf("%s requires a backend type", label)
	}
	switch stage.Type {
	case executor.BackendTypeCodexExec,
		executor.BackendTypeClaudeCode,
		executor.BackendTypeQwenCode,
		executor.BackendTypeAnthropicAPI,
		executor.BackendTypeOpenAIAPI,
		executor.BackendTypeOpenCode:
		return nil
	case "codex-app-server":
		return fmt.Errorf("%s %q is not a runnable Backend for architect mode; use codex-exec", label, stage.Type)
	default:
		return fmt.Errorf("%s %q is not a known backend (use one of: claude-code, codex-exec, qwen-code, anthropic-api, openai-api, opencode)", label, stage.Type)
	}
}

// architectBaseBackend returns the run's primary backend config, defaulting when
// the executor block is absent.
func architectBaseBackend(cfg *config.Config) executor.BackendConfig {
	if cfg.Executor != nil {
		return *cfg.Executor
	}
	return *executor.DefaultBackendConfig()
}

// architectQualityRunner builds a quality.Runner for the lint/coverage
// collectors, or nil when no quality config is present.
func architectQualityRunner(cfg *config.Config, projectDir string) *quality.Runner {
	if cfg.Quality == nil {
		return nil
	}
	return quality.NewRunner(cfg.Quality, projectDir)
}

// architectFailureSource opens the memory store backing the test-gap lens's
// bug-history collector. It is best-effort and fails open.
func architectFailureSource(cfg *config.Config) architect.FailureSource {
	if cfg.Memory == nil || cfg.Memory.Path == "" {
		return nil
	}
	store, err := memory.NewStore(cfg.Memory.Path)
	if err != nil || store == nil {
		return nil
	}
	return store
}

// architectKnowledgeSource opens the memory knowledge store backing the
// guardrail rule-suggester's pitfall/decision collectors. It mirrors
// architectFailureSource and fails open.
func architectKnowledgeSource(cfg *config.Config) architect.PitfallSource {
	if cfg.Memory == nil || cfg.Memory.Path == "" {
		return nil
	}
	store, err := memory.NewStore(cfg.Memory.Path)
	if err != nil || store == nil {
		return nil
	}
	ks := memory.NewKnowledgeStore(store.DB())
	if err := ks.InitSchema(); err != nil {
		return nil
	}
	return ks
}

// architectIssueClients builds the issue creator and dedup searcher from the
// GitHub adapter config, enabling issue creation on the client.
func architectIssueClients(cfg *config.Config) (architect.IssueCreator, architect.IssueSearcher, error) {
	token := architectGitHubToken(cfg)
	if token == "" {
		return nil, nil, fmt.Errorf("GitHub token not configured; set adapters.github.token or GITHUB_TOKEN to create issues")
	}
	client := github.NewClient(token)
	creator := architect.NewClientIssueCreator(client)
	return creator, client, nil
}

// architectGitHubToken resolves the GitHub token from config, falling back to
// the GITHUB_TOKEN environment variable.
func architectGitHubToken(cfg *config.Config) string {
	if cfg.Adapters != nil && cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Token != "" {
		return cfg.Adapters.GitHub.Token
	}
	return os.Getenv("GITHUB_TOKEN")
}

// architectOwnerRepo parses owner/repo from the GitHub adapter's "owner/repo"
// Repo field.
func architectOwnerRepo(cfg *config.Config) (owner, repo string, err error) {
	if cfg.Adapters == nil || cfg.Adapters.GitHub == nil || cfg.Adapters.GitHub.Repo == "" {
		return "", "", fmt.Errorf("no repository configured; set adapters.github.repo to owner/repo")
	}
	parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid adapters.github.repo %q; expected owner/repo", cfg.Adapters.GitHub.Repo)
	}
	return parts[0], parts[1], nil
}

// resolveArchitectLimit maps the --limit flag onto the emit cap.
func resolveArchitectLimit(flag int, ac *config.ArchitectConfig) int {
	if flag > 0 {
		return flag
	}
	if ac.MaxTickets > 0 {
		return ac.MaxTickets
	}
	return config.DefaultArchitectMaxTickets
}

// loadConfigForArchitect loads config from the resolved path.
func loadConfigForArchitect() (*config.Config, error) {
	cfg, err := config.Load(configPathOrDefault())
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// configPathOrDefault returns the active config path, defaulting when the
// global --config flag is unset.
func configPathOrDefault() string {
	if cfgFile != "" {
		return cfgFile
	}
	return config.DefaultConfigPath()
}
