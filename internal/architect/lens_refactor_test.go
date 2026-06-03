package architect

import (
	"reflect"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestRefactorLens_Registered(t *testing.T) {
	l, err := LensByName(RefactorLensName)
	if err != nil {
		t.Fatalf("refactor lens not registered: %v", err)
	}
	if l.Name != RefactorLensName {
		t.Fatalf("lens name = %q, want %q", l.Name, RefactorLensName)
	}
	if l.Slant == nil {
		t.Fatal("refactor lens must carry a slant")
	}
	if !contains(LensNames(), RefactorLensName) {
		t.Fatalf("refactor lens must appear in LensNames(): %v", LensNames())
	}
}

// TestRefactorLens_SelectableCaseInsensitive proves `--lens refactor` (and
// case/whitespace variants) resolves to the lens, exactly like the CLI flag path.
func TestRefactorLens_SelectableCaseInsensitive(t *testing.T) {
	for _, name := range []string{"refactor", "Refactor", "  REFACTOR  "} {
		l, err := LensByName(name)
		if err != nil {
			t.Fatalf("LensByName(%q): %v", name, err)
		}
		if l.Name != RefactorLensName {
			t.Fatalf("LensByName(%q) = %q, want %q", name, l.Name, RefactorLensName)
		}
	}
}

// TestRefactorLens_BundlesRefactorCollectors proves the lens roster is the core
// collectors plus the dependency-doctor collector (the blast-radius source).
func TestRefactorLens_BundlesRefactorCollectors(t *testing.T) {
	l, _ := LensByName(RefactorLensName)
	got := names(l.Collectors("/proj", ScanOptions{}))
	want := map[string]bool{
		"loc_over_400":      true,
		"todo_fixme":        true,
		kindDuplicateBlock:  true,
		"lint":              true,
		"dependency_doctor": true,
	}
	if len(got) != len(want) {
		t.Fatalf("refactor collectors = %v, want %d", got, len(want))
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("unexpected refactor collector %q (got %v)", n, got)
		}
	}
}

func TestRefactorLens_BuildLensScanner(t *testing.T) {
	s, err := BuildLensScanner("refactor", "/proj", ScanOptions{})
	if err != nil {
		t.Fatalf("build refactor scanner: %v", err)
	}
	if len(s.Collectors()) != 5 {
		t.Fatalf("refactor scanner collectors = %d, want 5: %v", len(s.Collectors()), names(s.Collectors()))
	}
}

func TestRefactorSlant_ContentAimsAtOrderedPRs(t *testing.T) {
	s := refactorSlant
	if s.headingOr("x") != refactorHeading {
		t.Fatalf("heading = %q", s.headingOr("x"))
	}
	if !containsSub(s.extraInstruction(), "ORDER") || !containsSub(s.extraInstruction(), "pilot-safe") {
		t.Fatalf("refactor instruction must demand ordering and a pilot-safe verdict: %q", s.extraInstruction())
	}
}

// TestPlanRefactorOffline_EndToEnd proves the offline path synthesizes findings
// from deterministic signals and projects them onto an ordered, blast-radius
// driven plan — no LLM, no network.
func TestPlanRefactorOffline_EndToEnd(t *testing.T) {
	g := planGraph()
	signals := []Signal{
		{Kind: "loc_over_400", File: "internal/leaf/big.go", Detail: "420 lines", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "loc_over_400", File: "internal/core/big.go", Detail: "510 lines", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "todo_fixme", File: "internal/mid/x.go", Detail: "TODO: clean up", Weight: 1, Risk: pilotapi.RiskLow},
	}

	plan := PlanRefactorOffline(signals, g, nil)
	if len(plan.PRs) == 0 {
		t.Fatal("offline plan must produce PRs from the signals")
	}
	// Orders are contiguous and ascending by blast radius (non-decreasing).
	prevBlast := -1
	for i, pr := range plan.PRs {
		if pr.Order != i+1 {
			t.Fatalf("PR[%d].Order = %d, want %d", i, pr.Order, i+1)
		}
		if pr.BlastRadius < prevBlast {
			t.Fatalf("PRs not ordered leaf-first: PR[%d] blast %d < prev %d", i, pr.BlastRadius, prevBlast)
		}
		prevBlast = pr.BlastRadius
	}
	if plan.Epic == nil || len(plan.Epic.Subtasks) != len(plan.PRs) {
		t.Fatalf("epic projection must mirror the PR sequence: %+v", plan.Epic)
	}
}

// TestPlanRefactorOffline_Deterministic proves the offline planner is
// reproducible across runs (the dry-run contract).
func TestPlanRefactorOffline_Deterministic(t *testing.T) {
	g := planGraph()
	signals := []Signal{
		{Kind: "loc_over_400", File: "internal/core/a.go", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "loc_over_400", File: "internal/mid/b.go", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "todo_fixme", File: "internal/leaf/c.go", Weight: 1, Risk: pilotapi.RiskLow},
		{Kind: kindLayerViolation, File: "internal/top/d.go", Weight: 1, Risk: pilotapi.RiskHigh},
	}
	first := PlanRefactorOffline(signals, g, nil)
	for i := 0; i < 5; i++ {
		if again := PlanRefactorOffline(signals, g, nil); !reflect.DeepEqual(first, again) {
			t.Fatalf("offline planner non-deterministic on run %d", i)
		}
	}
}

func TestPlanRefactorOffline_EmptySignals(t *testing.T) {
	plan := PlanRefactorOffline(nil, planGraph(), nil)
	if len(plan.PRs) != 0 {
		t.Fatalf("no signals => %d PRs, want 0", len(plan.PRs))
	}
	if plan.Epic == nil {
		t.Fatal("epic must be non-nil even with no signals")
	}
}

// TestPlanRefactorOffline_NilGraph proves the planner tolerates a nil graph
// (every change becomes a zero-blast leaf) so dry-run works without `go list`.
func TestPlanRefactorOffline_NilGraph(t *testing.T) {
	signals := []Signal{
		{Kind: "loc_over_400", File: "pkg/a.go", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "todo_fixme", File: "pkg/b.go", Weight: 1, Risk: pilotapi.RiskLow},
	}
	plan := PlanRefactorOffline(signals, nil, nil)
	if len(plan.PRs) == 0 {
		t.Fatal("nil graph must still yield PRs")
	}
	for _, pr := range plan.PRs {
		if pr.BlastRadius != 0 {
			t.Fatalf("nil graph => every PR is zero-blast, got %d for %q", pr.BlastRadius, pr.Title)
		}
	}
}
