package architect

import (
	"reflect"
	"testing"
)

// TestAssignOrderAndDeps_MultiHopChainAcrossTiers exercises assignOrderAndDeps
// directly over a hand-built, pre-sorted PR slice spanning four distinct blast
// tiers with sibling PRs sharing a tier. It proves:
//   - Order is the contiguous 1-indexed position;
//   - a step UP in blast radius gates the PR behind its immediate predecessor's
//     Order (the multi-hop leaf->...->root chain), NOT behind the lowest tier;
//   - equal-blast siblings stay independent (no fabricated edge);
//   - the DependsOn slice is reset (never appended) on each call.
func TestAssignOrderAndDeps_MultiHopChainAcrossTiers(t *testing.T) {
	// Pre-sorted leaf-first, as sortPlannedPRs would leave them. Tiers:
	//   blast 0: leafA, leafB (siblings)
	//   blast 1: mid
	//   blast 3: hi
	//   blast 3: hiSibling (same tier as hi)
	//   blast 7: top
	prs := []PlannedPR{
		{Title: "leafA", BlastRadius: 0, DependsOn: []int{99}}, // stale edge must be cleared
		{Title: "leafB", BlastRadius: 0},
		{Title: "mid", BlastRadius: 1},
		{Title: "hi", BlastRadius: 3},
		{Title: "hiSibling", BlastRadius: 3},
		{Title: "top", BlastRadius: 7},
	}

	assignOrderAndDeps(prs)

	// Orders are contiguous 1..6 in slice order.
	for i, pr := range prs {
		if pr.Order != i+1 {
			t.Fatalf("PR[%d] %q Order = %d, want %d", i, pr.Title, pr.Order, i+1)
		}
	}

	wantDeps := map[string][]int{
		"leafA":     nil,      // first PR is always a root
		"leafB":     nil,      // equal blast (0 == 0) -> independent sibling
		"mid":       {2},      // 1 > 0: gated behind immediate predecessor leafB (Order 2)
		"hi":        {3},      // 3 > 1: gated behind mid (Order 3)
		"hiSibling": nil,      // 3 == 3: equal-tier sibling, independent
		"top":       {5},      // 7 > 3: gated behind hiSibling (Order 5), the immediate predecessor
	}
	for _, pr := range prs {
		want := wantDeps[pr.Title]
		if !reflect.DeepEqual(pr.DependsOn, want) {
			t.Fatalf("PR %q DependsOn = %v, want %v (edge must be the immediate predecessor, not transitive)", pr.Title, pr.DependsOn, want)
		}
	}
}

// TestAssignOrderAndDeps_StrictlyAscendingChainsEveryHop proves the dense case:
// when every PR has a strictly higher blast radius than the one before, each PR
// (after the first) depends on exactly its immediate predecessor, forming an
// unbroken 1->2->3->...->n hop chain.
func TestAssignOrderAndDeps_StrictlyAscendingChainsEveryHop(t *testing.T) {
	prs := []PlannedPR{
		{Title: "t0", BlastRadius: 0},
		{Title: "t1", BlastRadius: 1},
		{Title: "t2", BlastRadius: 2},
		{Title: "t3", BlastRadius: 4},
	}

	assignOrderAndDeps(prs)

	if prs[0].DependsOn != nil {
		t.Fatalf("root PR must have no deps, got %v", prs[0].DependsOn)
	}
	for i := 1; i < len(prs); i++ {
		want := []int{prs[i-1].Order}
		if !reflect.DeepEqual(prs[i].DependsOn, want) {
			t.Fatalf("PR[%d] %q DependsOn = %v, want %v", i, prs[i].Title, prs[i].DependsOn, want)
		}
	}
}
