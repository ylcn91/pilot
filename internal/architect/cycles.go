package architect

import "sort"

// StronglyConnected returns the non-trivial strongly connected components of
// the graph: every set of two-or-more packages that are mutually reachable
// (i.e. a real import cycle). Single-package components are omitted because a
// lone package is only "in a cycle" if it imports itself, which the graph
// construction already excludes. Each component is returned sorted, and the
// list of components is sorted by its first member for determinism.
//
// It uses Tarjan's algorithm (iterative, so deep graphs cannot overflow the
// goroutine stack), running in O(V+E).
func (g *PackageGraph) StronglyConnected() [][]string {
	t := &tarjan{
		graph:   g,
		index:   make(map[string]int, len(g.nodes)),
		lowlink: make(map[string]int, len(g.nodes)),
		onStack: make(map[string]bool, len(g.nodes)),
	}

	for _, path := range g.Nodes() {
		if _, seen := t.index[path]; !seen {
			t.run(path)
		}
	}

	var components [][]string
	for _, comp := range t.components {
		if len(comp) >= 2 {
			sort.Strings(comp)
			components = append(components, comp)
		}
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i][0] < components[j][0]
	})
	return components
}

// HasCycle reports whether the graph contains any import cycle among its
// packages.
func (g *PackageGraph) HasCycle() bool {
	return len(g.StronglyConnected()) > 0
}

// tarjan holds the mutable state for an iterative Tarjan SCC pass.
type tarjan struct {
	graph      *PackageGraph
	counter    int
	index      map[string]int
	lowlink    map[string]int
	onStack    map[string]bool
	stack      []string
	components [][]string
}

// frame is one node's slot on the explicit DFS work stack. childIdx tracks how
// many of the node's out-edges have already been explored across re-entries.
type frame struct {
	node     string
	childIdx int
}

// run performs an iterative Tarjan DFS rooted at start, recording any SCCs it
// closes. Iterative (not recursive) so a very deep import chain cannot blow the
// stack — the project has thousands of packages.
func (t *tarjan) run(start string) {
	work := []frame{{node: start}}
	t.visit(start)

	for len(work) > 0 {
		top := &work[len(work)-1]
		node := top.node
		children := t.childrenOf(node)

		if top.childIdx < len(children) {
			child := children[top.childIdx]
			top.childIdx++
			if _, seen := t.index[child]; !seen {
				t.visit(child)
				work = append(work, frame{node: child})
			} else if t.onStack[child] {
				if t.index[child] < t.lowlink[node] {
					t.lowlink[node] = t.index[child]
				}
			}
			continue
		}

		// All children explored: maybe close an SCC, then return to parent.
		if t.lowlink[node] == t.index[node] {
			t.closeComponent(node)
		}
		work = work[:len(work)-1]
		if len(work) > 0 {
			parent := work[len(work)-1].node
			if t.lowlink[node] < t.lowlink[parent] {
				t.lowlink[parent] = t.lowlink[node]
			}
		}
	}
}

// visit assigns the next index/lowlink to node and pushes it onto the SCC
// stack.
func (t *tarjan) visit(node string) {
	t.index[node] = t.counter
	t.lowlink[node] = t.counter
	t.counter++
	t.stack = append(t.stack, node)
	t.onStack[node] = true
}

// closeComponent pops the SCC rooted at root off the stack into a component.
func (t *tarjan) closeComponent(root string) {
	var comp []string
	for {
		n := len(t.stack) - 1
		w := t.stack[n]
		t.stack = t.stack[:n]
		t.onStack[w] = false
		comp = append(comp, w)
		if w == root {
			break
		}
	}
	t.components = append(t.components, comp)
}

// childrenOf returns the in-graph out-edges of node in a stable sorted order so
// the SCC pass is deterministic.
func (t *tarjan) childrenOf(node string) []string {
	n, ok := t.graph.nodes[node]
	if !ok {
		return nil
	}
	children := append([]string(nil), n.Imports...)
	sort.Strings(children)
	return children
}
