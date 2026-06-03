package architect

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// TestIsRefactorLens covers the case-insensitive, whitespace-tolerant matching
// the CLI routing relies on, plus the negative cases (other lenses, empty).
func TestIsRefactorLens(t *testing.T) {
	for _, name := range []string{"refactor", "Refactor", "REFACTOR", "  refactor  "} {
		if !IsRefactorLens(name) {
			t.Fatalf("IsRefactorLens(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "core", "rfc", "depdoctor", "radar", "refactorr", "re factor"} {
		if IsRefactorLens(name) {
			t.Fatalf("IsRefactorLens(%q) = true, want false", name)
		}
	}
}

// TestRenderRefactorPlan_Empty proves an empty plan yields an explicit "no
// units" line and never a bare header.
func TestRenderRefactorPlan_Empty(t *testing.T) {
	out := RenderRefactorPlan(RefactorPlan{})
	if !strings.Contains(out, "No refactor units were surfaced") {
		t.Fatalf("empty plan must explain there is nothing to do, got:\n%s", out)
	}
	if strings.Contains(out, "1.") {
		t.Fatalf("empty plan must not number any PRs, got:\n%s", out)
	}
}

// TestRenderRefactorPlan_HeaderCounts proves the header reports the total PR
// count and the pilot-safe vs manual-review split derived from the plan.
func TestRenderRefactorPlan_HeaderCounts(t *testing.T) {
	plan := RefactorPlan{
		PRs: []PlannedPR{
			{Title: "A", Class: classMoveOnly, Order: 1, PilotSafe: true},
			{Title: "B", Class: classBehavior, Order: 2, PilotSafe: false, ManualReason: "high blast"},
			{Title: "C", Class: classCleanup, Order: 3, PilotSafe: true},
		},
		Manual: 1,
	}
	out := RenderRefactorPlan(plan)
	want := "Refactor plan: 3 ordered PR(s) — 2 pilot-safe, 1 manual-review"
	if !strings.Contains(out, want) {
		t.Fatalf("header mismatch.\nwant substring: %q\ngot:\n%s", want, out)
	}
	if !strings.Contains(out, "blast-radius ordered, leaves first") {
		t.Fatalf("header must state the ordering invariant, got:\n%s", out)
	}
}

// TestRenderRefactorPlan_PerPRLines proves each PR renders one line carrying its
// order, title, class, blast radius, verdict, dependencies, and reviewer.
func TestRenderRefactorPlan_PerPRLines(t *testing.T) {
	plan := RefactorPlan{
		PRs: []PlannedPR{
			{Title: "Split big.go", Class: classMoveOnly, Order: 1, BlastRadius: 0, PilotSafe: true, Owner: "alice"},
			{
				Title: "Break cycle", Class: classBehavior, Order: 2, BlastRadius: 7,
				PilotSafe: false, ManualReason: "high blast radius (7 dependent packages)",
				DependsOn: []int{1}, Owner: "bob",
			},
		},
		Manual: 1,
	}
	out := RenderRefactorPlan(plan)

	wantLine1 := "1. Split big.go — class=move-only, blast=0, pilot-safe, reviewer: alice"
	if !strings.Contains(out, wantLine1) {
		t.Fatalf("PR1 line missing.\nwant: %q\ngot:\n%s", wantLine1, out)
	}
	for _, frag := range []string{
		"2. Break cycle", "class=behavior", "blast=7",
		"manual-review (high blast radius (7 dependent packages))",
		"depends on PR 1", "reviewer: bob",
	} {
		if !strings.Contains(out, frag) {
			t.Fatalf("PR2 line missing %q, got:\n%s", frag, out)
		}
	}
}

// TestRenderRefactorPlan_OrderingMonotonic proves the rendered numbered lines
// appear in ascending Order, matching the plan's sequence.
func TestRenderRefactorPlan_OrderingMonotonic(t *testing.T) {
	plan := RefactorPlan{
		PRs: []PlannedPR{
			{Title: "first", Class: classMoveOnly, Order: 1, PilotSafe: true},
			{Title: "second", Class: classCleanup, Order: 2, PilotSafe: true},
			{Title: "third", Class: classBehavior, Order: 3, PilotSafe: true},
		},
	}
	out := RenderRefactorPlan(plan)
	re := regexp.MustCompile(`(?m)^(\d+)\.`)
	matches := re.FindAllStringSubmatch(out, -1)
	if len(matches) != 3 {
		t.Fatalf("expected 3 numbered lines, got %d:\n%s", len(matches), out)
	}
	for i, m := range matches {
		if m[1] != fmt.Sprintf("%d", i+1) {
			t.Fatalf("line %d numbered %q, want %d:\n%s", i, m[1], i+1, out)
		}
	}
}

// TestRenderRefactorPlan_NoOwnerNoDeps proves a leaf PR with no owner renders no
// trailing reviewer/depends clauses (terse line).
func TestRenderRefactorPlan_NoOwnerNoDeps(t *testing.T) {
	plan := RefactorPlan{
		PRs: []PlannedPR{{Title: "leaf", Class: classCleanup, Order: 1, BlastRadius: 0, PilotSafe: true}},
	}
	out := RenderRefactorPlan(plan)
	if strings.Contains(out, "reviewer:") {
		t.Fatalf("no owner => no reviewer clause, got:\n%s", out)
	}
	if strings.Contains(out, "depends on") {
		t.Fatalf("leaf => no depends clause, got:\n%s", out)
	}
	if !strings.Contains(out, "1. leaf — class=cleanup, blast=0, pilot-safe") {
		t.Fatalf("unexpected leaf line:\n%s", out)
	}
}

// TestRenderRefactorPlan_MatchesOfflinePlan is the end-to-end worst-case: real
// signals projected through the offline planner must render an ordered sequence
// whose lines are non-decreasing in blast radius (the leaf-first invariant).
func TestRenderRefactorPlan_MatchesOfflinePlan(t *testing.T) {
	g := planGraph()
	signals := []Signal{
		{Kind: "loc_over_400", File: "internal/leaf/big.go", Detail: "420 lines", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "loc_over_400", File: "internal/core/big.go", Detail: "510 lines", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: kindLayerViolation, File: "internal/top/x.go", Weight: 1, Risk: pilotapi.RiskHigh},
		{Kind: "todo_fixme", File: "internal/mid/y.go", Detail: "TODO: clean", Weight: 1, Risk: pilotapi.RiskLow},
	}
	plan := PlanRefactorOffline(signals, g, nil)
	out := RenderRefactorPlan(plan)
	if len(plan.PRs) == 0 {
		t.Fatal("offline plan produced no PRs from real signals")
	}
	// Every PR title from the plan must appear as a numbered line.
	for _, pr := range plan.PRs {
		line := fmt.Sprintf("%d. %s — class=%s, blast=%d", pr.Order, pr.Title, pr.Class, pr.BlastRadius)
		if !strings.Contains(out, line) {
			t.Fatalf("rendered output missing plan line %q\ngot:\n%s", line, out)
		}
	}
	// Blast radii encoded in the lines are non-decreasing (leaf-first).
	re := regexp.MustCompile(`blast=(\d+)`)
	prev := -1
	for _, m := range re.FindAllStringSubmatch(out, -1) {
		var b int
		fmt.Sscanf(m[1], "%d", &b)
		if b < prev {
			t.Fatalf("rendered sequence not leaf-first: blast %d after %d in:\n%s", b, prev, out)
		}
		prev = b
	}
}

// TestRenderRefactorPlan_Deterministic proves repeated renders of the same plan
// are byte-identical (the dry-run reproducibility contract).
func TestRenderRefactorPlan_Deterministic(t *testing.T) {
	plan := PlanRefactorOffline([]Signal{
		{Kind: "loc_over_400", File: "internal/core/a.go", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "todo_fixme", File: "internal/leaf/b.go", Weight: 1, Risk: pilotapi.RiskLow},
	}, planGraph(), map[string]string{"internal/core/a.go": "carol"})
	first := RenderRefactorPlan(plan)
	for i := 0; i < 5; i++ {
		if again := RenderRefactorPlan(plan); again != first {
			t.Fatalf("render non-deterministic on run %d:\nfirst:\n%s\nagain:\n%s", i, first, again)
		}
	}
}
