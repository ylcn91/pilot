package architect

import (
	"reflect"
	"testing"
)

// sampleNodes builds a small acyclic layered graph:
//
//	api  (leaf, no imports)
//	util -> api
//	core -> util, api
//	cmd  -> core, util
//
// plus an external (third-party) import on core that must be partitioned out.
func sampleNodes() []PackageNode {
	return []PackageNode{
		{ImportPath: "x/api"},
		{ImportPath: "x/util", Imports: []string{"x/api"}},
		{ImportPath: "x/core", Imports: []string{"x/util", "x/api", "github.com/3p/lib"}},
		{ImportPath: "x/cmd", Imports: []string{"x/core", "x/util"}},
	}
}

func TestNewPackageGraph_PartitionsExternalImports(t *testing.T) {
	g := NewPackageGraph(sampleNodes())

	core, ok := g.Node("x/core")
	if !ok {
		t.Fatal("core node missing")
	}
	wantInternal := map[string]bool{"x/util": true, "x/api": true}
	if len(core.Imports) != 2 {
		t.Fatalf("core internal imports = %v, want 2 internal", core.Imports)
	}
	for _, imp := range core.Imports {
		if !wantInternal[imp] {
			t.Errorf("unexpected internal edge %q", imp)
		}
	}
	foundExternal := false
	for _, e := range core.ExternalImports {
		if e == "github.com/3p/lib" {
			foundExternal = true
		}
	}
	if !foundExternal {
		t.Errorf("external import not partitioned into ExternalImports: %v", core.ExternalImports)
	}
}

func TestNewPackageGraph_DropsSelfEdge(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		{ImportPath: "x/a", Imports: []string{"x/a", "x/b"}},
		{ImportPath: "x/b"},
	})
	a, _ := g.Node("x/a")
	for _, imp := range a.Imports {
		if imp == "x/a" {
			t.Fatal("self-edge must be dropped from internal imports")
		}
	}
}

func TestPackageGraph_Nodes_SortedCopy(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	got := g.Nodes()
	want := []string{"x/api", "x/cmd", "x/core", "x/util"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Nodes() = %v, want sorted %v", got, want)
	}
	got[0] = "mutated"
	if g.Nodes()[0] != "x/api" {
		t.Fatal("mutating Nodes() result must not affect the graph")
	}
}

func TestPackageGraph_DependsOn(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	cases := []struct {
		from, to string
		want     bool
	}{
		{"x/core", "x/util", true},
		{"x/core", "x/api", true},
		{"x/util", "x/core", false},
		{"x/api", "x/util", false},
		{"x/missing", "x/api", false},
	}
	for _, c := range cases {
		if got := g.DependsOn(c.from, c.to); got != c.want {
			t.Errorf("DependsOn(%q,%q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestPackageGraph_Dependents(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	got := g.Dependents("x/api")
	want := []string{"x/core", "x/util"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dependents(api) = %v, want %v", got, want)
	}
	if got := g.Dependents("x/cmd"); len(got) != 0 {
		t.Errorf("Dependents(cmd) = %v, want empty (nothing imports cmd)", got)
	}
	if got := g.Dependents("x/unknown"); len(got) != 0 {
		t.Errorf("Dependents(unknown) = %v, want empty", got)
	}
}

func TestPackageGraph_BlastRadius_Transitive(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	// api is imported by util & core; cmd imports core & util.
	// So changing api can ripple to util, core, and cmd.
	got := g.BlastRadius("x/api")
	want := []string{"x/cmd", "x/core", "x/util"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BlastRadius(api) = %v, want %v", got, want)
	}
}

func TestPackageGraph_BlastRadius_Leaf(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	// Nothing imports cmd: zero blast radius.
	if got := g.BlastRadius("x/cmd"); len(got) != 0 {
		t.Fatalf("BlastRadius(cmd) = %v, want empty", got)
	}
}

func TestPackageGraph_BlastRadius_Unknown(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	if got := g.BlastRadius("x/nope"); got != nil {
		t.Fatalf("BlastRadius(unknown) = %v, want nil", got)
	}
}

func TestPackageGraph_BlastRadius_ExcludesSelf(t *testing.T) {
	g := NewPackageGraph(sampleNodes())
	for _, p := range g.BlastRadius("x/api") {
		if p == "x/api" {
			t.Fatal("blast radius must exclude the target itself")
		}
	}
}

// TestPackageGraph_BlastRadius_TerminatesOnCycle proves the reverse BFS does
// not loop forever on a cyclic graph (worst case for naive traversal).
func TestPackageGraph_BlastRadius_TerminatesOnCycle(t *testing.T) {
	g := NewPackageGraph([]PackageNode{
		{ImportPath: "c/a", Imports: []string{"c/b"}},
		{ImportPath: "c/b", Imports: []string{"c/a"}}, // a <-> b cycle
		{ImportPath: "c/x", Imports: []string{"c/a"}},
	})
	got := g.BlastRadius("c/a")
	want := []string{"c/b", "c/x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BlastRadius on cyclic graph = %v, want %v", got, want)
	}
}

func TestPackageGraph_EmptyGraph(t *testing.T) {
	g := NewPackageGraph(nil)
	if len(g.Nodes()) != 0 {
		t.Fatal("empty graph must have no nodes")
	}
	if g.HasCycle() {
		t.Fatal("empty graph has no cycle")
	}
	if got := g.BlastRadius("anything"); got != nil {
		t.Fatalf("blast radius on empty graph = %v, want nil", got)
	}
}
