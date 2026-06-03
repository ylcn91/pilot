package architect

import (
	"fmt"
	"reflect"
	"testing"
)

func TestStronglyConnected_NoCycleOnAcyclic(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	if g.HasCycle() {
		t.Fatal("layered acyclic graph must report no cycle")
	}
	if comps := g.StronglyConnected(); len(comps) != 0 {
		t.Fatalf("acyclic graph SCCs = %v, want none", comps)
	}
}

func TestStronglyConnected_TwoNodeCycle(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		{ImportPath: "x/a", Imports: []string{"x/b"}},
		{ImportPath: "x/b", Imports: []string{"x/a"}},
	})
	if !g.HasCycle() {
		t.Fatal("a<->b must be a cycle")
	}
	comps := g.StronglyConnected()
	if len(comps) != 1 {
		t.Fatalf("want 1 SCC, got %d: %v", len(comps), comps)
	}
	want := []string{"x/a", "x/b"}
	if !reflect.DeepEqual(comps[0], want) {
		t.Fatalf("SCC = %v, want %v (sorted)", comps[0], want)
	}
}

func TestStronglyConnected_ThreeNodeCycle(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		{ImportPath: "x/a", Imports: []string{"x/b"}},
		{ImportPath: "x/b", Imports: []string{"x/c"}},
		{ImportPath: "x/c", Imports: []string{"x/a"}},
	})
	comps := g.StronglyConnected()
	if len(comps) != 1 || len(comps[0]) != 3 {
		t.Fatalf("want one 3-node SCC, got %v", comps)
	}
	if !reflect.DeepEqual(comps[0], []string{"x/a", "x/b", "x/c"}) {
		t.Fatalf("SCC members = %v", comps[0])
	}
}

func TestStronglyConnected_MultipleDisjointCycles(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		// cycle 1: a<->b
		{ImportPath: "p/a", Imports: []string{"p/b"}},
		{ImportPath: "p/b", Imports: []string{"p/a"}},
		// cycle 2: m<->n
		{ImportPath: "p/m", Imports: []string{"p/n"}},
		{ImportPath: "p/n", Imports: []string{"p/m"}},
		// acyclic bridge
		{ImportPath: "p/z", Imports: []string{"p/a", "p/m"}},
	})
	comps := g.StronglyConnected()
	if len(comps) != 2 {
		t.Fatalf("want 2 disjoint cycles, got %d: %v", len(comps), comps)
	}
	// Components sorted by first member: {a,b} then {m,n}.
	if !reflect.DeepEqual(comps[0], []string{"p/a", "p/b"}) {
		t.Errorf("first SCC = %v", comps[0])
	}
	if !reflect.DeepEqual(comps[1], []string{"p/m", "p/n"}) {
		t.Errorf("second SCC = %v", comps[1])
	}
}

// TestStronglyConnected_NestedSCC verifies a larger strongly connected
// component (everything mutually reachable) collapses into a single SCC.
func TestStronglyConnected_NestedSCC(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		{ImportPath: "n/a", Imports: []string{"n/b"}},
		{ImportPath: "n/b", Imports: []string{"n/c"}},
		{ImportPath: "n/c", Imports: []string{"n/a", "n/d"}},
		{ImportPath: "n/d", Imports: []string{"n/b"}},
	})
	comps := g.StronglyConnected()
	if len(comps) != 1 {
		t.Fatalf("want 1 SCC, got %v", comps)
	}
	if !reflect.DeepEqual(comps[0], []string{"n/a", "n/b", "n/c", "n/d"}) {
		t.Fatalf("SCC = %v, want all four", comps[0])
	}
}

// TestStronglyConnected_DeepChainNoStackOverflow constructs a long acyclic
// chain to exercise the iterative DFS — a recursive Tarjan would risk a stack
// overflow on a 10k-deep import chain.
func TestStronglyConnected_DeepChainNoStackOverflow(t *testing.T) {
	const n = 10000
	nodes := make([]PackageNode, n)
	for i := 0; i < n; i++ {
		node := PackageNode{ImportPath: fmt.Sprintf("d/p%05d", i)}
		if i+1 < n {
			node.Imports = []string{fmt.Sprintf("d/p%05d", i+1)}
		}
		nodes[i] = node
	}
	g := NewPackageGraph(nodes)
	if g.HasCycle() {
		t.Fatal("deep linear chain has no cycle")
	}
}

// TestStronglyConnected_DeepCycleDetected makes the deep chain cyclic by
// closing the last node back to the first; the whole chain becomes one SCC.
func TestStronglyConnected_DeepCycleDetected(t *testing.T) {
	const n = 5000
	nodes := make([]PackageNode, n)
	for i := 0; i < n; i++ {
		next := (i + 1) % n
		nodes[i] = PackageNode{
			ImportPath: fmt.Sprintf("d/p%05d", i),
			Imports:    []string{fmt.Sprintf("d/p%05d", next)},
		}
	}
	g := NewPackageGraph(nodes)
	comps := g.StronglyConnected()
	if len(comps) != 1 || len(comps[0]) != n {
		t.Fatalf("want one SCC of size %d, got %d comps", n, len(comps))
	}
}

func TestStronglyConnected_SelfImportIsNotCycle(t *testing.T) {
	// Graph construction drops self-edges, so a lone self-importing package
	// is not reported as a cycle (no real two-package mutual dependency).
	g := NewPackageGraph([]PackageNode{
		{ImportPath: "s/a", Imports: []string{"s/a"}},
	})
	if g.HasCycle() {
		t.Fatal("dropped self-edge must not count as a cycle")
	}
}
