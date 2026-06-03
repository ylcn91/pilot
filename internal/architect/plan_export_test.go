package architect

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestFindingsFromRefactorPlanPreservesLineage(t *testing.T) {
	plan := RefactorPlan{PRs: []PlannedPR{
		{
			Title:        "Split core",
			Class:        classMoveOnly,
			Order:        2,
			DependsOn:    []int{1},
			BlastRadius:  3,
			PilotSafe:    false,
			ManualReason: "high blast radius",
			Owner:        "Ada",
			Files:        []string{"internal/core/a.go"},
			Risk:         pilotapi.RiskHigh,
		},
	}}

	got := FindingsFromRefactorPlan(plan)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	f := got[0]
	if f.Title != "Split core" || f.Kind != "refactor" || f.Risk != pilotapi.RiskHigh {
		t.Fatalf("unexpected finding header: %+v", f)
	}
	for _, want := range []string{"Order: PR 2", "Blast radius: 3", "MANUAL REVIEW REQUIRED", "Ada"} {
		if !strings.Contains(f.WhyItMatters, want) {
			t.Fatalf("WhyItMatters missing %q:\n%s", want, f.WhyItMatters)
		}
	}
	if len(f.SuggestedPRPieces) != 3 || !strings.Contains(f.SuggestedPRPieces[1], "PR 1") {
		t.Fatalf("SuggestedPRPieces missing dependency/manual lineage: %+v", f.SuggestedPRPieces)
	}
	if f.TestPlan != refactorPlanTestPlan {
		t.Fatalf("TestPlan = %q, want %q", f.TestPlan, refactorPlanTestPlan)
	}
}

func TestEmitterEmitOrderedBypassesRiskRanking(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "o", "r", nil)
	findings := []pilotapi.Finding{
		finding("Low first", "refactor", pilotapi.RiskLow, "a.go"),
		finding("High second", "refactor", pilotapi.RiskHigh, "b.go"),
	}

	created, skipped, err := e.EmitOrdered(context.Background(), findings, false, 0)
	if err != nil {
		t.Fatalf("EmitOrdered: %v", err)
	}
	if created != 2 || skipped != 0 {
		t.Fatalf("created=%d skipped=%d, want 2/0", created, skipped)
	}
	if len(creator.calls) != 2 || !strings.Contains(creator.calls[0].title, "Low first") {
		t.Fatalf("EmitOrdered must keep caller order, calls=%+v", creator.calls)
	}
}
