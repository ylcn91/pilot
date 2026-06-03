package architect

import (
	"context"
	"fmt"
	"sort"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Signal kinds emitted by the dependency-doctor collector.
const (
	kindImportCycle    = "import_cycle_risk"
	kindLayerViolation = "layer_violation"
	kindUnusedDep      = "unused_dep"
	kindHeavyDep       = "heavy_dep"
)

// heavyDepFanIn is the module-graph fan-in at or above which a dependency is
// flagged "heavy": many modules require it, so it is broadly entrenched and
// expensive to remove or swap. Tuned to surface the genuinely load-bearing
// third-party libraries, not every leaf.
const heavyDepFanIn = 8

// DepsCollector is the keystone of the dependency-doctor lens. It builds the
// project's package import graph from `go list -deps -json ./...` and emits
// graph-shaped Signals: import-cycle risk (strongly connected package groups),
// layer violations (forbidden edges, seeded from currently-satisfied rules),
// unused dependencies (`go mod why` == not needed), and heavy dependencies
// (high module-graph fan-in).
//
// It is best-effort: every external tool call degrades gracefully. If
// `go list` fails entirely, the collector returns no Signals and no error
// (an empty graph), so a broken toolchain never aborts the scan.
type DepsCollector struct {
	run        commandRunner
	rules      []LayerRule
	heavyFanIn int
}

// NewDepsCollector returns a DepsCollector wired to the real `go` toolchain and
// the default layer rules. Tests construct a collector with newDepsCollector to
// inject a mock runner and custom rules.
func NewDepsCollector() *DepsCollector {
	return newDepsCollector(execCommandRunner, defaultLayerRules, heavyDepFanIn)
}

// newDepsCollector is the injectable constructor used by tests.
func newDepsCollector(run commandRunner, rules []LayerRule, heavyFanIn int) *DepsCollector {
	return &DepsCollector{run: run, rules: rules, heavyFanIn: heavyFanIn}
}

// Name implements Collector.
func (c *DepsCollector) Name() string { return "dependency_doctor" }

// Collect builds the package graph for projectPath and emits the dependency
// Signals. Each sub-analysis is independent and best-effort: a failure in one
// (e.g. `go mod graph` unavailable) does not suppress the others. The returned
// error is non-nil only when the graph could not be built at all AND the
// caller would otherwise get a misleading empty result — in practice the
// Scanner tolerates either way.
func (c *DepsCollector) Collect(ctx context.Context, projectPath string) ([]Signal, error) {
	modulePrefix := modulePathFromDir(projectPath)

	graph, err := loadPackageGraph(ctx, c.run, projectPath, modulePrefix)
	if err != nil {
		// Degrade gracefully: no graph means no graph-shaped signals, but the
		// scan as a whole must not fail. Return the error so the Scanner logs
		// it; the Scanner treats a collector error as "skip", not "abort".
		return nil, fmt.Errorf("dependency_doctor: build graph: %w", err)
	}

	var signals []Signal
	signals = append(signals, c.cycleSignals(graph)...)
	signals = append(signals, c.layerSignals(graph, modulePrefix)...)
	signals = append(signals, c.heavySignals(ctx, projectPath)...)
	signals = append(signals, c.unusedSignals(ctx, projectPath)...)
	return signals, nil
}

// cycleSignals emits one import_cycle_risk Signal per strongly connected
// package group (a real or near cycle among internal packages). Weight scales
// with the size of the cluster; risk is high for any genuine cycle since it
// blocks clean layering and incremental builds.
func (c *DepsCollector) cycleSignals(graph *PackageGraph) []Signal {
	var signals []Signal
	for _, comp := range graph.StronglyConnected() {
		signals = append(signals, Signal{
			Kind:   kindImportCycle,
			File:   comp[0],
			Detail: fmt.Sprintf("import cycle among %d packages: %s", len(comp), joinList(comp)),
			Weight: float64(len(comp)),
			Risk:   pilotapi.RiskHigh,
		})
	}
	return signals
}

// layerSignals emits one layer_violation Signal per forbidden edge found.
// Because the rules are seeded from currently-satisfied directions, any Signal
// here is a real regression. Risk is high: a layering inversion is a structural
// defect, not a style nit.
func (c *DepsCollector) layerSignals(graph *PackageGraph, modulePrefix string) []Signal {
	var signals []Signal
	for _, v := range CheckLayerRules(graph, modulePrefix, c.rules) {
		signals = append(signals, Signal{
			Kind:   kindLayerViolation,
			File:   v.From,
			Detail: fmt.Sprintf("forbidden import %s -> %s (rule %s: %s)", v.From, v.To, v.Rule.Name, v.Rule.Reason),
			Weight: 5,
			Risk:   pilotapi.RiskHigh,
		})
	}
	return signals
}

// heavySignals emits one heavy_dep Signal per module whose module-graph fan-in
// meets the threshold. Best-effort: a `go mod graph` failure yields no Signals.
// Heavy deps are informational (medium risk): they are entrenched, not broken.
func (c *DepsCollector) heavySignals(ctx context.Context, projectPath string) []Signal {
	out, err := c.run(ctx, projectPath, "go", "mod", "graph")
	if err != nil && len(out) == 0 {
		return nil
	}
	fanIn := parseModGraph(out)

	type heavy struct {
		mod   string
		count int
	}
	var heavies []heavy
	for mod, count := range fanIn {
		if count >= c.heavyFanIn {
			heavies = append(heavies, heavy{mod, count})
		}
	}
	sort.Slice(heavies, func(i, j int) bool {
		if heavies[i].count != heavies[j].count {
			return heavies[i].count > heavies[j].count
		}
		return heavies[i].mod < heavies[j].mod
	})

	signals := make([]Signal, 0, len(heavies))
	for _, h := range heavies {
		signals = append(signals, Signal{
			Kind:   kindHeavyDep,
			File:   h.mod,
			Detail: fmt.Sprintf("heavy dependency: %d modules require %s", h.count, h.mod),
			Weight: float64(h.count) / float64(c.heavyFanIn),
			Risk:   pilotapi.RiskMedium,
		})
	}
	return signals
}

// unusedSignals probes every required module with `go mod why` and emits an
// unused_dep Signal for each one the main module no longer needs. Best-effort:
// modules whose `why` call errors are skipped, never flagged. Unused deps are
// low risk (dead weight, not a defect) but high signal-to-noise for cleanup.
func (c *DepsCollector) unusedSignals(ctx context.Context, projectPath string) []Signal {
	mods := requiredModules(projectPath)
	sort.Strings(mods)

	var signals []Signal
	for _, mod := range mods {
		if err := ctx.Err(); err != nil {
			break
		}
		if isModuleUnused(ctx, c.run, projectPath, mod) {
			signals = append(signals, Signal{
				Kind:   kindUnusedDep,
				File:   mod,
				Detail: fmt.Sprintf("module %s is required but not imported (go mod why: not needed)", mod),
				Weight: 1,
				Risk:   pilotapi.RiskLow,
			})
		}
	}
	return signals
}

// joinList renders a string slice as a comma-separated list for Signal detail.
func joinList(items []string) string {
	const maxItems = 6
	if len(items) <= maxItems {
		return commaJoin(items)
	}
	head := commaJoin(items[:maxItems])
	return fmt.Sprintf("%s, +%d more", head, len(items)-maxItems)
}

// commaJoin joins items with ", ".
func commaJoin(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
