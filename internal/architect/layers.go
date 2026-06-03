package architect

import "sort"

// PackageNode is one vertex in the project's internal import graph: a single
// Go package identified by its full import path, the module it belongs to, and
// the set of packages it directly imports (edges out). Only edges to packages
// the graph knows about are retained; standard-library and third-party imports
// are recorded separately as ExternalImports so layer/cycle analysis stays
// scoped to the project's own packages.
type PackageNode struct {
	// ImportPath is the full package import path (graph vertex id).
	ImportPath string
	// Module is the module the package belongs to (may be empty for the
	// standard library).
	Module string
	// Imports are the in-graph packages this package directly imports.
	Imports []string
	// ExternalImports are imported paths that are not project packages
	// (stdlib + third-party). Kept for heavy/unused analysis, not traversal.
	ExternalImports []string
}

// PackageGraph is the project's internal import graph: a set of PackageNodes
// keyed by import path, with traversal limited to edges between known nodes.
// It is the shared substrate the dependency-doctor collector reasons over —
// cycle detection, layer rules, and blast-radius all run against it.
type PackageGraph struct {
	nodes map[string]*PackageNode
}

// NewPackageGraph builds a graph from the given nodes, keyed by ImportPath.
// Each node's Imports are partitioned: edges to paths present in the node set
// are kept as in-graph Imports; everything else is moved to ExternalImports.
// This makes the graph self-consistent (no dangling internal edges) regardless
// of how the caller populated Imports.
func NewPackageGraph(nodes []PackageNode) *PackageGraph {
	known := make(map[string]bool, len(nodes))
	for i := range nodes {
		known[nodes[i].ImportPath] = true
	}

	g := &PackageGraph{nodes: make(map[string]*PackageNode, len(nodes))}
	for i := range nodes {
		n := nodes[i]
		var internal, external []string
		for _, imp := range n.Imports {
			if known[imp] && imp != n.ImportPath {
				internal = append(internal, imp)
			} else if imp != n.ImportPath {
				external = append(external, imp)
			}
		}
		external = append(external, n.ExternalImports...)
		g.nodes[n.ImportPath] = &PackageNode{
			ImportPath:      n.ImportPath,
			Module:          n.Module,
			Imports:         internal,
			ExternalImports: external,
		}
	}
	return g
}

// Nodes returns the graph's package import paths in sorted order. The result
// is a fresh slice; mutating it does not affect the graph.
func (g *PackageGraph) Nodes() []string {
	out := make([]string, 0, len(g.nodes))
	for path := range g.nodes {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// Node returns the node for the given import path and whether it exists.
func (g *PackageGraph) Node(path string) (*PackageNode, bool) {
	n, ok := g.nodes[path]
	return n, ok
}

// DependsOn reports whether package "from" imports package "to" directly.
func (g *PackageGraph) DependsOn(from, to string) bool {
	n, ok := g.nodes[from]
	if !ok {
		return false
	}
	for _, imp := range n.Imports {
		if imp == to {
			return true
		}
	}
	return false
}

// Dependents returns the import paths of every package that directly imports
// target (reverse edges, sorted). An unknown target yields an empty slice.
func (g *PackageGraph) Dependents(target string) []string {
	var out []string
	for path, n := range g.nodes {
		for _, imp := range n.Imports {
			if imp == target {
				out = append(out, path)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// BlastRadius returns every package that transitively depends on target
// (target excluded), i.e. the full set of packages a change to target could
// ripple into. The result is sorted and excludes target itself. An unknown
// target yields an empty slice.
//
// It walks the reverse graph breadth-first from target, so the cost is linear
// in the number of edges and it terminates even on cyclic graphs.
func (g *PackageGraph) BlastRadius(target string) []string {
	if _, ok := g.nodes[target]; !ok {
		return nil
	}

	reverse := g.reverseAdjacency()
	seen := map[string]bool{target: true}
	queue := append([]string(nil), reverse[target]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		queue = append(queue, reverse[cur]...)
	}

	out := make([]string, 0, len(seen))
	for path := range seen {
		if path != target {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// reverseAdjacency builds target -> [packages importing target] for every edge
// in the graph. Used by blast-radius / reverse-dependency traversals.
func (g *PackageGraph) reverseAdjacency() map[string][]string {
	rev := make(map[string][]string, len(g.nodes))
	for path, n := range g.nodes {
		for _, imp := range n.Imports {
			rev[imp] = append(rev[imp], path)
		}
	}
	return rev
}
