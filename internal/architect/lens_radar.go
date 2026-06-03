package architect

// RadarLensName is the selector for the radar lens.
const RadarLensName = "radar"

// radarCollectors builds the radar lens roster: the deterministic core
// collectors (oversized files, TODO/FIXME, lint, optional coverage) plus the
// dependency-doctor collector (import-cycle / layer-drift signals), the
// stale-test collector (tests that have fallen behind their source), the
// duplication collector (copy-pasted code blocks), and the churn collector
// (recurring execution failures as churn hotspots). It is the widest
// deterministic roster — the radar's job is to surface architectural drift and
// tech-debt from every angle in a single periodic scan.
//
// Every collector in the roster degrades gracefully on its own (a nil quality
// runner, an absent `go` toolchain, a non-git directory, or a nil failure
// source each yields zero signals rather than an error), so the radar lens is
// safe to run unattended on a schedule against any project state.
func radarCollectors(_ string, opts ScanOptions) []Collector {
	collectors := coreCollectors(opts)
	collectors = append(collectors, NewDepsCollector())
	collectors = append(collectors, NewStaleTestsCollector())
	collectors = append(collectors, NewDuplicationCollector())
	collectors = append(collectors, NewChurnCollector(opts.FailureSource, opts.FailureQuery, 0, opts.ProjectID))
	return collectors
}

// init registers the radar lens. It is selectable via `pilot architect --lens
// radar` and is the roster the scheduler drives to keep the dashboard's
// findings sink fresh: a periodic, deterministic sweep for architectural drift
// (cycles, layer violations, heavy/unused deps), oversized files, TODO/FIXME
// debt, stale tests, and churn hotspots, all ranked onto the shared
// pilotapi.Finding scale.
func init() {
	RegisterLens(Lens{
		Name:        RadarLensName,
		Description: "radar: periodic drift + tech-debt sweep (core signals + dependency cycles/drift + stale tests + duplicated blocks + churn hotspots) for the dashboard sink",
		Collectors:  radarCollectors,
	})
}
