package architect

// TestGapLensName is the selector for the test-gap designer lens.
const TestGapLensName = "testgap"

// testGapTaskDescription is the .agent guidance hint for the test-gap lens, so
// the overlay machinery surfaces testing SOPs/conventions rather than refactor
// context.
const testGapTaskDescription = "design a missing-test matrix and critical-path coverage plan"

// testGapHeading and testGapIntro frame the PROPOSE prompt for the test-gap
// lens: the goal is coverage, not refactoring.
const (
	testGapHeading = "Test-Gap Designer"
	testGapIntro   = "A deterministic scan surfaced the test gaps below: areas that keep " +
		"breaking (bug hotspots), packages under the coverage threshold, and recently-changed " +
		"files with no tests at all. Treat these as the input to a coverage plan, not a refactor."
)

// testGapInstruction is the lens's analyzer slant: it demands a missing-test
// matrix, a critical-path list, race/e2e suggestions, and a fixture-reuse plan,
// all carried in each finding's test_plan (the primary payload of a test-gap
// finding). It is appended after the signal summary so the backend reasons over
// the concrete gaps before producing the matrix.
const testGapInstruction = "For each cluster of related gaps, produce one finding whose test_plan is the " +
	"PRIMARY payload and contains:\n" +
	"  1. A missing-test MATRIX: rows = the untested behaviours/branches, columns = the " +
	"test level that should cover each (unit | table-driven | integration | e2e).\n" +
	"  2. A CRITICAL-PATH list: the highest-risk untested code paths to cover first, " +
	"ordered by the bug-hotspot and coverage signals.\n" +
	"  3. RACE and E2E suggestions: where `-race` runs or end-to-end flows would catch the " +
	"classes of failure seen in the bug_hotspot signals.\n" +
	"  4. A FIXTURE-REUSE plan: existing test fixtures/helpers in the package to reuse so the " +
	"new tests stay small and consistent.\n" +
	"Set kind to \"test-gap\". Keep suggested_pr_pieces to small, independently-mergeable test PRs."

// testGapSlant is the shared slant the test-gap lens applies to the PROPOSE
// stage. It is package-level (not rebuilt per call) because it is immutable.
var testGapSlant = &LensSlant{
	TaskDescription:  testGapTaskDescription,
	Heading:          testGapHeading,
	Intro:            testGapIntro,
	ExtraInstruction: testGapInstruction,
}

// testGapCollectors builds the test-gap lens roster: the bug-history collector
// (recurring failures from memory), the coverage collector reused from the core
// spine (low_coverage), and the changed-files-without-tests collector
// (missing_test). The bug-history and coverage collectors stay inert when their
// backing source/runner is absent, so the lens degrades gracefully.
func testGapCollectors(_ string, opts ScanOptions) []Collector {
	collectors := []Collector{
		NewBugHistoryCollector(opts.FailureSource, opts.FailureQuery, 0, opts.ProjectID),
		NewMissingTestsCollector(),
	}
	// Reuse the spine's coverage collector. It is only meaningful with both a
	// runner and a positive threshold; otherwise it would be inert noise.
	if runner := gateRunnerFromQuality(opts.QualityRunner); runner != nil && opts.MinCoverage > 0 {
		collectors = append(collectors, NewCoverageCollector(runner, "coverage", opts.MinCoverage))
	}
	return collectors
}

// init registers the test-gap designer lens. It bundles the bug-history,
// coverage, and missing-tests collectors over the shared SCAN spine and carries
// the test-gap analyzer slant so `pilot architect --lens testgap` produces a
// missing-test matrix and coverage plan rather than refactor proposals.
func init() {
	RegisterLens(Lens{
		Name:        TestGapLensName,
		Description: "test-gap designer: bug-history hotspots, low coverage, changed files without tests; emits a missing-test matrix",
		Collectors:  testGapCollectors,
		Slant:       testGapSlant,
	})
}
