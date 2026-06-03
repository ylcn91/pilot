package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
	"github.com/ylcn91/pilot/internal/quality"
)

// architectFlags are the parsed CLI knobs for `pilot architect`.
type architectFlags struct {
	dryRun       bool
	createIssues bool
	limit        int
	backend      string
	jsonOut      bool
	lens         string
}

// newArchitectCmd builds the `pilot architect` command: it runs the proactive
// SCAN -> PROPOSE -> EMIT pipeline against the repo and reports the ranked
// findings. It defaults to dry-run (no issues filed); --create-issues opts into
// real issue creation.
func newArchitectCmd() *cobra.Command {
	f := &architectFlags{dryRun: true}

	cmd := &cobra.Command{
		Use:   "architect",
		Short: "Proactively scan the repo and propose refactor tickets",
		Long: `Architect scans this repository for refactor signals (oversized files,
TODO/FIXME, lint, low coverage), asks the configured backend to propose the
highest-value changes, and (optionally) files them as deduplicated GitHub issues.

Dry-run by default — nothing is created unless --create-issues is passed.

Examples:
  pilot architect                       # dry-run: print proposals, create nothing
  pilot architect --create-issues       # file the top proposals as issues
  pilot architect --limit 3 --json      # cap to 3, emit machine-readable output`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// --create-issues is the explicit opt-in to real creation; it flips
			// off the dry-run default.
			if f.createIssues {
				f.dryRun = false
			}
			return runArchitect(cmd.Context(), f)
		},
	}

	cmd.Flags().BoolVar(&f.dryRun, "dry-run", true, "Compute and print proposals without creating issues")
	cmd.Flags().BoolVar(&f.createIssues, "create-issues", false, "File the top proposals as GitHub issues (disables dry-run)")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "Max proposals to emit (0 uses architect.max_tickets or 10)")
	cmd.Flags().StringVar(&f.backend, "backend", "", "Override the PROPOSE backend type (e.g. claude-code, codex-exec)")
	cmd.Flags().BoolVar(&f.jsonOut, "json", false, "Emit findings as JSON")
	cmd.Flags().StringVar(&f.lens, "lens", "", fmt.Sprintf("Collector lens to run (default %q; e.g. depdoctor). Available: %s", architect.CoreLensName, strings.Join(architect.LensNames(), ", ")))

	return cmd
}

// runArchitect loads config, wires the pipeline, runs it, and prints the result.
func runArchitect(ctx context.Context, f *architectFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, err := loadConfigForArchitect()
	if err != nil {
		return err
	}

	ac := cfg.Architect
	if ac == nil || !ac.Enabled {
		return fmt.Errorf("architect is not enabled; set architect.enabled: true in %s", configPathOrDefault())
	}

	agentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}

	runCfg, err := buildArchitectRunConfig(cfg, agentDir, f)
	if err != nil {
		return err
	}

	opts := architect.RunOptions{
		DryRun: f.dryRun,
		Limit:  resolveArchitectLimit(f.limit, ac),
	}

	result, err := architect.Run(ctx, runCfg, opts)
	if err != nil {
		return err
	}

	return printArchitectResult(result, f)
}

// buildArchitectRunConfig assembles the SCAN scanner, PROPOSE analyzer, and EMIT
// target from cfg. When --create-issues is set it builds an issue-creating
// GitHub client; otherwise (dry-run) it leaves the creator nil so no network
// call is made.
func buildArchitectRunConfig(cfg *config.Config, agentDir string, f *architectFlags) (architect.RunConfig, error) {
	ac := cfg.Architect

	// Resolve the lens once so its slant can aim the PROPOSE prompt; an unknown
	// --lens surfaces here before any work is done.
	lens, err := architect.LensByName(f.lens)
	if err != nil {
		return architect.RunConfig{}, err
	}

	scanner, err := architect.BuildLensScanner(f.lens, agentDir, architect.ScanOptions{
		QualityRunner: architectQualityRunner(cfg, agentDir),
		MinCoverage:   ac.Thresholds.MinCoverage,
		Signals:       ac.Signals,
		FailureSource: architectFailureSource(cfg),
		ProjectID:     agentDir,
	})
	if err != nil {
		return architect.RunConfig{}, err
	}

	analyzer := architect.NewAnalyzer(
		architectBackendStage(ac, f.backend),
		architectBaseBackend(cfg),
		agentDir,
		architect.WithSlant(lens.Slant),
	)

	rc := architect.RunConfig{
		Scanner:     scanner,
		Analyzer:    analyzer,
		ProjectPath: agentDir,
		Labels:      ac.Labels,
	}

	// Owner/repo are only needed when we actually file (or dedup against) issues.
	owner, repo, repoErr := architectOwnerRepo(cfg)
	if !f.dryRun {
		if repoErr != nil {
			return architect.RunConfig{}, repoErr
		}
		creator, searcher, err := architectIssueClients(cfg)
		if err != nil {
			return architect.RunConfig{}, err
		}
		rc.Creator = creator
		rc.Searcher = searcher
	}
	rc.Owner = owner
	rc.Repo = repo

	return rc, nil
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

// architectBaseBackend returns the run's primary backend config, defaulting when
// the executor block is absent.
func architectBaseBackend(cfg *config.Config) executor.BackendConfig {
	if cfg.Executor != nil {
		return *cfg.Executor
	}
	return *executor.DefaultBackendConfig()
}

// architectQualityRunner builds a quality.Runner for the lint/coverage
// collectors, or nil when no quality config is present (leaving those
// collectors inert).
func architectQualityRunner(cfg *config.Config, projectDir string) *quality.Runner {
	if cfg.Quality == nil {
		return nil
	}
	return quality.NewRunner(cfg.Quality, projectDir)
}

// architectFailureSource opens the memory store backing the test-gap lens's
// bug-history collector. It is best-effort: a missing memory config or an
// unopenable store yields a nil source, leaving that collector inert rather
// than failing the scan. The concrete *memory.Store is returned as the
// failureSource interface; a nil store is returned as an untyped nil so the
// collector's nil check fires correctly.
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

// resolveArchitectLimit maps the --limit flag onto the emit cap: an explicit
// positive flag wins; 0 falls back to architect.max_tickets (or the package
// default when that is unset).
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

// printArchitectResult renders the run outcome as human-readable text or JSON.
func printArchitectResult(result architect.RunResult, f *architectFlags) error {
	if f.jsonOut {
		return printArchitectJSON(result, f.dryRun)
	}
	printArchitectHuman(result, f.dryRun)
	return nil
}

// architectJSONReport is the stable shape emitted by --json.
type architectJSONReport struct {
	DryRun   bool               `json:"dry_run"`
	Created  int                `json:"created"`
	Skipped  int                `json:"skipped"`
	Findings []pilotapi.Finding `json:"findings"`
}

func printArchitectJSON(result architect.RunResult, dryRun bool) error {
	report := architectJSONReport{
		DryRun:   dryRun,
		Created:  result.Created,
		Skipped:  result.Skipped,
		Findings: result.Findings,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printArchitectHuman(result architect.RunResult, dryRun bool) {
	mode := "created"
	if dryRun {
		mode = "would create"
	}
	fmt.Printf("Architect found %d proposal(s).\n", len(result.Findings))
	for i, f := range result.Findings {
		risk := f.Risk
		if risk == "" {
			risk = pilotapi.RiskMedium
		}
		fmt.Printf("\n%d. [%s] %s\n", i+1, risk, f.Title)
		if f.WhyItMatters != "" {
			fmt.Printf("   why: %s\n", f.WhyItMatters)
		}
		if len(f.Files) > 0 {
			fmt.Printf("   files: %s\n", strings.Join(f.Files, ", "))
		}
	}
	fmt.Printf("\n%s %d issue(s), skipped %d (dedup).\n", mode, result.Created, result.Skipped)
}
