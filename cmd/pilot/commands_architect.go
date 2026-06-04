package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/logging"
)

// architectFlags are the parsed CLI knobs for `pilot architect`.
type architectFlags struct {
	dryRun       bool
	createIssues bool
	limit        int
	backend      string
	jsonOut      bool
	lens         string
	export       string
	linearParent string
	suggestRules bool
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
	cmd.Flags().StringVar(&f.export, "export", "", fmt.Sprintf("Where to file proposals: %s (default %q, or architect.export). 'adr' applies to the rfc lens.", strings.Join(config.ValidArchitectExports, "|"), config.DefaultArchitectExport))
	cmd.Flags().StringVar(&f.linearParent, "linear-parent", "", "Existing Linear issue ID to file sub-issues under (required when --export linear)")
	cmd.Flags().BoolVar(&f.suggestRules, "suggest-rules", false, "Surface advisory DRAFT guardrail rules mined from recorded pitfalls/decisions (never enforced; human review required)")

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
	if err := validateArchitectBackendStage(architectBackendStage(ac, f.backend), architectBackendLabel(f)); err != nil {
		return err
	}

	agentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}

	export, err := architectExportTarget(ac, f.export)
	if err != nil {
		return err
	}

	// The RFC-generator lens produces a document, not a list of issues, so it
	// takes its own render/write path rather than the SCAN->PROPOSE->EMIT
	// pipeline. --export adr selects this document path explicitly.
	if architect.IsRFCLens(f.lens) {
		return runArchitectRFC(ctx, cfg, agentDir, f)
	}

	// adr export only makes sense for the RFC lens (the only path that writes a
	// document); reject it for the issue-filing lenses rather than silently
	// ignoring it.
	if export == config.ArchitectExportADR {
		return fmt.Errorf("--export adr is only valid with --lens %s", architect.RFCLensName)
	}

	// The refactor lens's dry-run headline is the ordered, blast-radius-driven
	// tiny-PR sequence (not the generic flat finding list), so the dry-run path
	// renders the offline RefactorPlan directly. --create-issues keeps the
	// SCAN->PROPOSE->EMIT pipeline below so the same plan can be filed as issues.
	if architect.IsRefactorLens(f.lens) && f.dryRun {
		return runArchitectRefactor(ctx, cfg, agentDir, f)
	}
	if architect.IsRefactorLens(f.lens) && export == config.ArchitectExportLinear {
		return runArchitectRefactorLinear(ctx, cfg, agentDir, f)
	}

	runCfg, err := buildArchitectRunConfig(cfg, agentDir, f)
	if err != nil {
		return err
	}

	opts := architect.RunOptions{
		DryRun:               f.dryRun,
		SuppressDryRunOutput: f.jsonOut,
		Limit:                resolveArchitectLimit(f.limit, ac),
	}

	if f.jsonOut {
		logging.Suppress()
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
		SuggestRules:  f.suggestRules,
		Memory: architect.MemoryOptions{
			FailureSource:   architectFailureSource(cfg),
			ProjectID:       agentDir,
			KnowledgeSource: architectKnowledgeSource(cfg),
		},
	})
	if err != nil {
		return architect.RunConfig{}, err
	}

	stage := architectBackendStage(ac, f.backend)
	if err := validateArchitectBackendStage(stage, architectBackendLabel(f)); err != nil {
		return architect.RunConfig{}, err
	}

	// Dry-run uses the deterministic, network-free PROPOSE path so a scan yields
	// real graph-derived findings without spawning an LLM subprocess. Issue
	// creation (--create-issues) keeps the backend-driven analysis.
	analyzer := architect.NewAnalyzer(
		stage,
		architectBaseBackend(cfg),
		agentDir,
		architect.WithSlant(lens.Slant),
		architect.WithOffline(f.dryRun),
	)

	rc := architect.RunConfig{
		Scanner:     scanner,
		Analyzer:    analyzer,
		ProjectPath: agentDir,
		Labels:      ac.Labels,
	}

	export, err := architectExportTarget(ac, f.export)
	if err != nil {
		return architect.RunConfig{}, err
	}

	// The Linear export target files sub-issues under an existing parent epic; it
	// derives team/project from that parent rather than a GitHub owner/repo.
	if export == config.ArchitectExportLinear {
		if !f.dryRun {
			creator, searcher, err := architectLinearClients(cfg, f.linearParent)
			if err != nil {
				return architect.RunConfig{}, err
			}
			rc.Creator = creator
			rc.Searcher = searcher
		}
		return rc, nil
	}

	// Owner/repo are only needed when we actually file (or dedup against) GitHub
	// issues.
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

// architectExportTarget resolves the export target: an explicit --export flag
// wins, then architect.export from config, else the GitHub default. An invalid
// value (from either source) is rejected here, mirroring the flag-over-config
// pattern of architectBackendStage.
func architectExportTarget(ac *config.ArchitectConfig, flag string) (string, error) {
	target := flag
	if target == "" {
		target = ac.Export
	}
	if target == "" {
		target = config.DefaultArchitectExport
	}
	switch target {
	case config.ArchitectExportGitHub, config.ArchitectExportLinear, config.ArchitectExportADR:
		return target, nil
	default:
		return "", fmt.Errorf("invalid export target %q; expected one of %s", target, strings.Join(config.ValidArchitectExports, ", "))
	}
}
