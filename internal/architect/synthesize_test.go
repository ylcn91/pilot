package architect

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// findingByKind returns the first finding whose Title contains substr, or fails.
func findingByTitle(t *testing.T, fs []pilotapi.Finding, substr string) pilotapi.Finding {
	t.Helper()
	for _, f := range fs {
		if strings.Contains(f.Title, substr) {
			return f
		}
	}
	t.Fatalf("no finding with title containing %q in %d findings", substr, len(fs))
	return pilotapi.Finding{}
}

// TestSynthesize_EmptySignals proves an empty input yields a non-nil, empty
// slice (never nil, never a panic).
func TestSynthesize_EmptySignals(t *testing.T) {
	got := SynthesizeFindings(nil)
	if got == nil {
		t.Fatal("SynthesizeFindings(nil) must return a non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("empty input must yield 0 findings, got %d", len(got))
	}
	if got2 := SynthesizeFindings([]Signal{}); len(got2) != 0 {
		t.Fatalf("empty slice must yield 0 findings, got %d", len(got2))
	}
}

// TestSynthesize_OneClusterPerKind proves Signals of distinct kinds become one
// Finding each, and same-kind Signals collapse into a single Finding.
func TestSynthesize_OneClusterPerKind(t *testing.T) {
	signals := []Signal{
		{Kind: "loc_over_400", File: "a.go", Risk: pilotapi.RiskHigh},
		{Kind: "loc_over_400", File: "b.go", Risk: pilotapi.RiskMedium},
		{Kind: "todo_fixme", File: "c.go", Line: 7, Risk: pilotapi.RiskLow},
	}
	got := SynthesizeFindings(signals)
	if len(got) != 2 {
		t.Fatalf("2 kinds => 2 findings, got %d: %+v", len(got), got)
	}
	loc := findingByTitle(t, got, "oversized")
	if len(loc.Files) != 2 {
		t.Errorf("loc finding should list both files, got %v", loc.Files)
	}
	// The loc cluster's max risk is high (a.go).
	if loc.Risk != pilotapi.RiskHigh {
		t.Errorf("loc finding risk = %q, want high", loc.Risk)
	}
}

// TestSynthesize_RiskIsClusterMax proves a Finding's Risk is the maximum across
// its cluster, not the first or last Signal's.
func TestSynthesize_RiskIsClusterMax(t *testing.T) {
	signals := []Signal{
		{Kind: "todo_fixme", File: "a.go", Risk: pilotapi.RiskLow},
		{Kind: "todo_fixme", File: "b.go", Risk: pilotapi.RiskReleaseBlocker},
		{Kind: "todo_fixme", File: "c.go", Risk: pilotapi.RiskMedium},
	}
	got := SynthesizeFindings(signals)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	if got[0].Risk != pilotapi.RiskReleaseBlocker {
		t.Errorf("cluster risk = %q, want release-blocker (the max)", got[0].Risk)
	}
}

// TestSynthesize_OrderingByRiskThenKind proves the emission order is descending
// by cluster max-risk, then ascending by kind.
func TestSynthesize_OrderingByRiskThenKind(t *testing.T) {
	signals := []Signal{
		{Kind: "unused_dep", File: "mod/low", Risk: pilotapi.RiskLow},
		{Kind: "import_cycle_risk", File: "pkg/high", Risk: pilotapi.RiskHigh},
		{Kind: "heavy_dep", File: "mod/med", Risk: pilotapi.RiskMedium},
	}
	got := SynthesizeFindings(signals)
	if len(got) != 3 {
		t.Fatalf("want 3 findings, got %d", len(got))
	}
	if got[0].Risk != pilotapi.RiskHigh {
		t.Errorf("first finding must be highest risk, got %q", got[0].Risk)
	}
	if got[2].Risk != pilotapi.RiskLow {
		t.Errorf("last finding must be lowest risk, got %q", got[2].Risk)
	}
}

// TestSynthesize_TieBreakByKindName proves two same-risk clusters are ordered by
// ascending kind name for stability.
func TestSynthesize_TieBreakByKindName(t *testing.T) {
	signals := []Signal{
		{Kind: "zebra_kind", File: "z.go", Risk: pilotapi.RiskMedium},
		{Kind: "alpha_kind", File: "a.go", Risk: pilotapi.RiskMedium},
	}
	got := SynthesizeFindings(signals)
	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d", len(got))
	}
	if !strings.Contains(got[0].Title, "alpha kind") {
		t.Errorf("alpha_kind should sort first, got %q then %q", got[0].Title, got[1].Title)
	}
}

// TestSynthesize_LocationFormatting proves File:Line is rendered when Line>0 and
// just File otherwise, and that duplicate locations collapse.
func TestSynthesize_LocationFormatting(t *testing.T) {
	signals := []Signal{
		{Kind: "todo_fixme", File: "api.go", Line: 10},
		{Kind: "todo_fixme", File: "api.go", Line: 10}, // exact dup
		{Kind: "todo_fixme", File: "core.go", Line: 0},
	}
	got := SynthesizeFindings(signals)
	f := got[0]
	if len(f.Files) != 2 {
		t.Fatalf("duplicate location must collapse to 2 files, got %v", f.Files)
	}
	// Sorted: "api.go:10" then "core.go".
	if f.Files[0] != "api.go:10" {
		t.Errorf("Line>0 must render File:Line, got %q", f.Files[0])
	}
	if f.Files[1] != "core.go" {
		t.Errorf("Line==0 must render bare File, got %q", f.Files[1])
	}
}

// TestSynthesize_DeterministicAcrossRuns proves identical Signals (even shuffled)
// yield byte-identical findings — the core offline guarantee.
func TestSynthesize_DeterministicAcrossRuns(t *testing.T) {
	a := []Signal{
		{Kind: "loc_over_400", File: "b.go", Risk: pilotapi.RiskHigh},
		{Kind: "loc_over_400", File: "a.go", Risk: pilotapi.RiskHigh},
		{Kind: "todo_fixme", File: "z.go", Line: 1, Risk: pilotapi.RiskLow},
	}
	b := []Signal{
		{Kind: "todo_fixme", File: "z.go", Line: 1, Risk: pilotapi.RiskLow},
		{Kind: "loc_over_400", File: "a.go", Risk: pilotapi.RiskHigh},
		{Kind: "loc_over_400", File: "b.go", Risk: pilotapi.RiskHigh},
	}
	fa := SynthesizeFindings(a)
	fb := SynthesizeFindings(b)
	if len(fa) != len(fb) {
		t.Fatalf("len mismatch %d vs %d", len(fa), len(fb))
	}
	for i := range fa {
		if fa[i].Title != fb[i].Title {
			t.Errorf("finding %d title differs: %q vs %q", i, fa[i].Title, fb[i].Title)
		}
		if strings.Join(fa[i].Files, ",") != strings.Join(fb[i].Files, ",") {
			t.Errorf("finding %d files differ: %v vs %v", i, fa[i].Files, fb[i].Files)
		}
	}
}

// TestSynthesize_KnownKindsGetTailoredFraming proves each catalogued kind gets a
// non-generic finding kind and a kind-specific test plan.
func TestSynthesize_KnownKindsGetTailoredFraming(t *testing.T) {
	cases := []struct {
		signalKind   string
		wantFindKind string
	}{
		{"import_cycle_risk", "refactor"},
		{"layer_violation", "hardening"},
		{"heavy_dep", "refactor"},
		{"unused_dep", "refactor"},
		{"loc_over_400", "refactor"},
		{"todo_fixme", "hardening"},
		{"low_coverage", "test-gap"},
		{"missing_test", "test-gap"},
		{"bug_hotspot", "test-gap"},
	}
	for _, tc := range cases {
		got := SynthesizeFindings([]Signal{{Kind: tc.signalKind, File: "x.go", Risk: pilotapi.RiskMedium}})
		if len(got) != 1 {
			t.Fatalf("%s: want 1 finding, got %d", tc.signalKind, len(got))
		}
		if string(got[0].Kind) != tc.wantFindKind {
			t.Errorf("%s: finding kind = %q, want %q", tc.signalKind, got[0].Kind, tc.wantFindKind)
		}
		if got[0].WhyItMatters == "" {
			t.Errorf("%s: WhyItMatters must not be empty", tc.signalKind)
		}
		if got[0].TestPlan == "" {
			t.Errorf("%s: TestPlan must not be empty", tc.signalKind)
		}
		if len(got[0].SuggestedPRPieces) == 0 {
			t.Errorf("%s: SuggestedPRPieces must not be empty", tc.signalKind)
		}
	}
}

// TestSynthesize_TestGapTestPlan proves test-gap findings get the coverage-flavoured
// plan, not the refactor one.
func TestSynthesize_TestGapTestPlan(t *testing.T) {
	got := SynthesizeFindings([]Signal{{Kind: "missing_test", File: "x.go", Risk: pilotapi.RiskMedium}})
	if !strings.Contains(got[0].TestPlan, "-race") || !strings.Contains(got[0].TestPlan, "coverage") {
		t.Errorf("test-gap plan should mention -race and coverage, got %q", got[0].TestPlan)
	}
}

// TestSynthesize_UnknownKindFallback proves a kind absent from the catalog still
// produces a usable, non-empty finding (no silent drop).
func TestSynthesize_UnknownKindFallback(t *testing.T) {
	got := SynthesizeFindings([]Signal{{Kind: "brand_new_kind", File: "x.go", Risk: pilotapi.RiskMedium}})
	if len(got) != 1 {
		t.Fatalf("unknown kind must still produce a finding, got %d", len(got))
	}
	f := got[0]
	if !strings.Contains(f.Title, "brand new kind") {
		t.Errorf("fallback title should humanise the kind, got %q", f.Title)
	}
	if f.Kind != "refactor" {
		t.Errorf("fallback finding kind = %q, want refactor", f.Kind)
	}
	if len(f.SuggestedPRPieces) == 0 || f.WhyItMatters == "" {
		t.Errorf("fallback finding must be fully populated: %+v", f)
	}
}

// TestSynthesize_EmptyKindSkipped proves a Signal with no Kind is dropped (it
// cannot be framed) rather than producing a degenerate finding.
func TestSynthesize_EmptyKindSkipped(t *testing.T) {
	got := SynthesizeFindings([]Signal{
		{Kind: "  ", File: "x.go", Risk: pilotapi.RiskHigh},
		{Kind: "todo_fixme", File: "y.go", Risk: pilotapi.RiskLow},
	})
	if len(got) != 1 {
		t.Fatalf("empty-kind signal must be skipped, got %d findings: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Title, "TODO") {
		t.Errorf("surviving finding should be the todo one, got %q", got[0].Title)
	}
}

// TestSynthesize_NoFileSignalSkippedFromLocations proves a Signal with no File
// contributes to the cluster's existence/risk but adds no Files entry.
func TestSynthesize_NoFileSignalSkippedFromLocations(t *testing.T) {
	got := SynthesizeFindings([]Signal{
		{Kind: "low_coverage", File: "", Risk: pilotapi.RiskHigh},
		{Kind: "low_coverage", File: "pkg/svc", Risk: pilotapi.RiskMedium},
	})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	if len(got[0].Files) != 1 || got[0].Files[0] != "pkg/svc" {
		t.Errorf("no-file signal must not add a Files entry, got %v", got[0].Files)
	}
	// Risk still reflects the no-file high signal.
	if got[0].Risk != pilotapi.RiskHigh {
		t.Errorf("cluster risk should still be high, got %q", got[0].Risk)
	}
}

// TestSynthesize_FilesAndPiecesCapped proves a huge cluster caps its Files and
// PR-pieces lists and records the overflow, so a worst-case scan never emits an
// unbounded issue body.
func TestSynthesize_FilesAndPiecesCapped(t *testing.T) {
	var signals []Signal
	for i := 0; i < 50; i++ {
		signals = append(signals, Signal{
			Kind: "loc_over_400",
			File: pad("file", i),
			Risk: pilotapi.RiskMedium,
		})
	}
	got := SynthesizeFindings(signals)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	f := got[0]
	if len(f.Files) != maxFilesPerFinding {
		t.Errorf("Files must cap at %d, got %d", maxFilesPerFinding, len(f.Files))
	}
	if len(f.SuggestedPRPieces) != maxPRPiecesPerFinding+1 {
		t.Errorf("PR pieces must cap at %d + 1 aggregate, got %d", maxPRPiecesPerFinding, len(f.SuggestedPRPieces))
	}
	// The title's count reflects the full cluster, not the capped Files.
	if !strings.Contains(f.Title, "50") {
		t.Errorf("title should report the full count of 50, got %q", f.Title)
	}
	if !strings.Contains(f.WhyItMatters, "not listed individually") {
		t.Errorf("overflow must be noted in WhyItMatters, got %q", f.WhyItMatters)
	}
	last := f.SuggestedPRPieces[len(f.SuggestedPRPieces)-1]
	if !strings.Contains(last, "remaining") {
		t.Errorf("last PR piece should collapse the tail, got %q", last)
	}
}

// TestSynthesize_AllUnknownRiskDefaultsMedium proves a cluster whose signals all
// carry an empty risk still gets a canonical medium risk.
func TestSynthesize_AllUnknownRiskDefaultsMedium(t *testing.T) {
	got := SynthesizeFindings([]Signal{
		{Kind: "todo_fixme", File: "a.go"},
		{Kind: "todo_fixme", File: "b.go"},
	})
	if got[0].Risk != pilotapi.RiskMedium {
		t.Errorf("empty-risk cluster must default to medium, got %q", got[0].Risk)
	}
}

// TestMaxRisk_Empty proves maxRisk on an empty group returns medium.
func TestMaxRisk_Empty(t *testing.T) {
	if r := maxRisk(nil); r != pilotapi.RiskMedium {
		t.Errorf("maxRisk(nil) = %q, want medium", r)
	}
}

// TestSignalLocation proves the location renderer's edge cases.
func TestSignalLocation(t *testing.T) {
	if got := signalLocation(Signal{File: "a.go", Line: 5}); got != "a.go:5" {
		t.Errorf("File+Line = %q, want a.go:5", got)
	}
	if got := signalLocation(Signal{File: "a.go"}); got != "a.go" {
		t.Errorf("File only = %q, want a.go", got)
	}
	if got := signalLocation(Signal{File: "  "}); got != "" {
		t.Errorf("blank file = %q, want empty", got)
	}
}

// pad builds a stable, unique filename for the capping test.
func pad(prefix string, i int) string {
	return prefix + "_" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + ".go"
}
