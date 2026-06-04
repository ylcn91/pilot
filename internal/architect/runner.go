package architect

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// defaultEmitLimit caps how many proposals a single Run files as issues when the
// caller does not specify one. The Architect is proactive and noisy by design at
// the SCAN stage; the EMIT stage stays conservative so one run never floods a
// repo with issues.
const defaultEmitLimit = 5

// RunConfig is the fully-assembled input to Run: the SCAN scanner, the PROPOSE
// analyzer, the EMIT target (owner/repo, labels, issue creator, dedup searcher),
// and the project path to scan. Constructing it (which wires the executor
// backend and github client) is the boundary's job; Run itself is pure
// orchestration so it stays trivially testable and shared by the CLI and the
// scheduler.
type RunConfig struct {
	// Scanner runs the deterministic SCAN collectors. Required.
	Scanner *Scanner
	// Analyzer runs the PROPOSE backend pass. Required.
	Analyzer *Analyzer
	// ProjectPath is the root the Scanner walks. Required.
	ProjectPath string

	// Owner and Repo are the issue target, parsed from cfg.Adapters.GitHub.Repo.
	Owner string
	Repo  string
	// Labels applied to filed issues; empty falls back to the default set.
	Labels []string
	// Creator files issues. May be nil to force dry-run-only behaviour.
	Creator IssueCreator
	// Searcher backs cross-run dedup. May be nil to skip the remote check.
	Searcher IssueSearcher
}

// RunOptions are per-invocation knobs that the CLI/scheduler vary between calls
// without rebuilding the RunConfig.
type RunOptions struct {
	// DryRun, when true, computes and reports proposals/would-be issues but
	// creates nothing.
	DryRun bool
	// SuppressDryRunOutput keeps dry-run issue previews out of stdout. It is
	// intended for JSON callers that need a clean machine-readable stream.
	SuppressDryRunOutput bool
	// Limit caps how many top-ranked proposals are emitted. Zero (the default)
	// applies defaultEmitLimit; a negative value means no cap.
	Limit int
}

// RunResult is the structured outcome of a Run: the proposals the PROPOSE stage
// produced (full, pre-limit set, in rank order), and the EMIT tallies.
type RunResult struct {
	Findings []pilotapi.Finding
	Created  int
	Skipped  int
}

// effectiveLimit resolves the emit cap: zero means "use the default", a negative
// value means "no cap" (passed through to Emit as <= 0).
func (o RunOptions) effectiveLimit() int {
	switch {
	case o.Limit == 0:
		return defaultEmitLimit
	case o.Limit < 0:
		return 0
	default:
		return o.Limit
	}
}

// Run executes the full Architect pipeline against cfg: SCAN the project for
// signals, PROPOSE ranked findings from them, then EMIT the top-N as idempotent
// issues (honouring opts.DryRun / opts.Limit). It is the single entry point
// shared by the CLI and the scheduler.
//
// The stages short-circuit gracefully: an empty signal set yields no proposals
// and no issues without invoking the backend; an empty proposal set yields no
// issues without touching GitHub. A failure in any stage aborts and is returned
// with context; SCAN's own per-collector failures are tolerated inside Scan and
// never surface here.
func Run(ctx context.Context, cfg RunConfig, opts RunOptions) (RunResult, error) {
	log := logging.WithComponent("architect.run")

	if cfg.Scanner == nil || cfg.Analyzer == nil {
		return RunResult{}, fmt.Errorf("architect: Run requires a Scanner and an Analyzer")
	}

	signals, err := cfg.Scanner.Scan(ctx, cfg.ProjectPath)
	if err != nil {
		return RunResult{}, fmt.Errorf("architect: scan: %w", err)
	}
	log.Info("scan complete", slog.Int("signals", len(signals)))

	findings, err := cfg.Analyzer.Propose(ctx, signals)
	if err != nil {
		return RunResult{}, fmt.Errorf("architect: propose: %w", err)
	}
	ranked := rankFindings(findings)
	log.Info("propose complete", slog.Int("findings", len(ranked)))

	emitter := NewEmitter(cfg.Creator, cfg.Searcher, cfg.Owner, cfg.Repo, cfg.Labels)
	if opts.SuppressDryRunOutput {
		emitter.SuppressDryRunOutput()
	}
	created, skipped, err := emitter.Emit(ctx, ranked, opts.DryRun, opts.effectiveLimit())
	if err != nil {
		return RunResult{Findings: ranked}, fmt.Errorf("architect: emit: %w", err)
	}
	log.Info("emit complete",
		slog.Int("created", created),
		slog.Int("skipped", skipped),
		slog.Bool("dry_run", opts.DryRun),
	)

	return RunResult{
		Findings: ranked,
		Created:  created,
		Skipped:  skipped,
	}, nil
}
