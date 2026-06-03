package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// rfcTitleDefault is the working title used for the generated RFC when no
// better one is supplied. It feeds both the document H1 and the on-disk slug.
const rfcTitleDefault = "Refactor RFC"

// runArchitectRFC is the RFC-generator lens's own entry point, branched out of
// runArchitect because the lens produces an ADR document rather than a list of
// issues. It scans for the same deterministic signals the refactor planner uses,
// loads the blast-radius graph, composes the RFC (offline-deterministic, with
// backend prose layered in only on the write path), prints it plus the
// would-write path, and writes `.agent/system/rfc_<slug>.md` only when
// --create-issues is set.
func runArchitectRFC(ctx context.Context, cfg *config.Config, agentDir string, f *architectFlags) error {
	signals, err := scanRFCSignals(ctx, cfg, agentDir, f)
	if err != nil {
		return err
	}

	// The blast-radius graph drives PR ordering; a missing `go` toolchain yields
	// a nil graph, which every planner tolerates (every change becomes a leaf).
	graph, _ := architect.LoadProjectGraph(ctx, agentDir)
	owners := refactorOwners(ctx, agentDir, signals)
	findings := architect.SynthesizeFindings(signals)
	plan := architect.ProjectToEpic(findings, graph, owners)

	doc := architect.BuildRFCDoc(rfcTitleDefault, findings, plan, architect.RFCProse{})

	export, err := architectExportTarget(cfg.Architect, f.export)
	if err != nil {
		return err
	}
	if export == config.ArchitectExportLinear {
		if f.dryRun {
			printRFCDryRun(doc, "Linear parent "+f.linearParent)
			return nil
		}
		return emitRefactorPlanToLinear(ctx, cfg, f, plan)
	}

	// On the write path, try to enrich the narrative sections with the
	// configured backend; any failure falls back silently to the offline doc.
	if !f.dryRun {
		if enriched, ok := enrichRFCWithBackend(ctx, cfg, agentDir, f, signals, findings, &plan); ok {
			doc = enriched
		}
	}

	// ADR documents land under the scanned project's .agent/system, not the
	// process CWD, so the write target is anchored to agentDir.
	adrDir := filepath.Join(agentDir, architect.ADRDir)
	wantPath := architect.ADRPath(adrDir, doc.Slug)
	if f.dryRun {
		printRFCDryRun(doc, wantPath)
		return nil
	}

	written, err := architect.WriteADR(adrDir, doc.Slug, doc.Body)
	if err != nil {
		return fmt.Errorf("write RFC: %w", err)
	}
	fmt.Printf("Wrote RFC to %s\n", written)
	return nil
}

// scanRFCSignals runs the rfc lens's scanner (the refactor roster) against
// agentDir and returns the deterministic signals.
func scanRFCSignals(ctx context.Context, cfg *config.Config, agentDir string, f *architectFlags) ([]architect.Signal, error) {
	ac := cfg.Architect
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
		return nil, err
	}
	return scanner.Scan(ctx, agentDir)
}

// enrichRFCWithBackend asks the configured backend (via the rfc slant) for the
// narrative sections and folds them into the RFC. It returns ok=false on any
// error or empty response so the caller keeps the offline document; the
// deterministic tiny-PR sequence is always preserved. The deterministic
// findings and plan are computed once by the caller and threaded through here
// so they are not recomputed.
func enrichRFCWithBackend(ctx context.Context, cfg *config.Config, agentDir string, f *architectFlags, signals []architect.Signal, findings []pilotapi.Finding, plan *architect.RefactorPlan) (architect.RFCDoc, bool) {
	lens, err := architect.LensByName(f.lens)
	if err != nil {
		return architect.RFCDoc{}, false
	}
	analyzer := architect.NewAnalyzer(
		architectBackendStage(cfg.Architect, f.backend),
		architectBaseBackend(cfg),
		agentDir,
		architect.WithSlant(lens.Slant),
	)
	proposed, err := analyzer.Propose(ctx, signals)
	if err != nil || len(proposed) == 0 {
		return architect.RFCDoc{}, false
	}
	prose := architect.ProseFromFindings(proposed)
	return architect.BuildRFCDoc(rfcTitleDefault, findings, *plan, prose), true
}

// printRFCDryRun prints the rendered RFC and the path it would be written to,
// without touching disk — the dry-run contract.
func printRFCDryRun(doc architect.RFCDoc, wantPath string) {
	fmt.Fprintln(os.Stdout, doc.Body)
	if wantPath == "" {
		return
	}
	fmt.Printf("\n(dry-run) would write/export the above RFC to %s\n", wantPath)
	fmt.Printf("(dry-run) pass --create-issues to write/export it.\n")
}
