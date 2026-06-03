package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
)

// runArchitectRefactor is the refactor lens's dry-run entry point, branched out
// of runArchitect because the headline output is an ORDERED tiny-PR sequence
// (blast-radius driven, pilot-safe flagged), not the generic flat finding list.
// It scans the same deterministic signals the RFC lens uses, loads the
// blast-radius graph, resolves best-effort file ownership for reviewer hints,
// projects the offline RefactorPlan, and prints the ordered sequence. It never
// touches the network or files disk — the dry-run contract — so --create-issues
// is routed back through the SCAN->PROPOSE->EMIT pipeline by the caller instead.
func runArchitectRefactor(ctx context.Context, cfg *config.Config, agentDir string, f *architectFlags) error {
	signals, err := scanRFCSignals(ctx, cfg, agentDir, f)
	if err != nil {
		return err
	}

	// The blast-radius graph drives PR ordering; a missing `go` toolchain yields
	// a nil graph, which the planner tolerates (every change becomes a leaf).
	graph, _ := architect.LoadProjectGraph(ctx, agentDir)

	owners := refactorOwners(ctx, agentDir, signals)

	plan := architect.PlanRefactorOffline(signals, graph, owners)
	printRefactorPlan(plan)
	return nil
}

// refactorOwners resolves a best-effort top-author-per-file map for the files
// the signals touch, so each planned PR can carry a suggested reviewer. It is
// degraded-gracefully: no git history (or no signals) yields an empty map and
// the plan simply omits reviewer hints.
func refactorOwners(ctx context.Context, agentDir string, signals []architect.Signal) map[string]string {
	paths := signalFiles(signals)
	if len(paths) == 0 {
		return nil
	}
	return architect.NewOwnershipCollector().Owners(ctx, agentDir, paths)
}

// signalFiles returns the distinct, non-empty file paths referenced by the
// signals, preserving first-seen order so the ownership probe is deterministic.
func signalFiles(signals []architect.Signal) []string {
	seen := make(map[string]bool, len(signals))
	var out []string
	for _, s := range signals {
		if s.File == "" || seen[s.File] {
			continue
		}
		seen[s.File] = true
		out = append(out, s.File)
	}
	return out
}

// printRefactorPlan renders the ordered PR sequence to stdout, prefixed with a
// dry-run banner so it is unmistakable that nothing was created.
func printRefactorPlan(plan architect.RefactorPlan) {
	fmt.Fprintln(os.Stdout, "--- architect (dry-run) ordered refactor PR sequence ---")
	fmt.Fprintln(os.Stdout)
	fmt.Fprint(os.Stdout, architect.RenderRefactorPlan(plan))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "(dry-run) nothing created; pass --create-issues to file this sequence as linked issues.")
}
