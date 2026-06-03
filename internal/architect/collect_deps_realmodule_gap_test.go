package architect

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// realGoModuleFixture writes a minimal, self-contained Go module under
// t.TempDir() with two internal packages where svc imports api. It returns the
// module root. The module is acyclic and compiles, so a real `go list -deps
// -json ./...` succeeds and exercises the full loadPackageGraph parse path
// against genuine toolchain output (not a hand-rolled fixture stream).
func realGoModuleFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "go.mod", "module example.com/realproj\n\ngo 1.24\n")
	writeFile(t, dir, filepath.Join("internal", "api", "api.go"),
		"package api\n\nfunc Greet() string { return \"hi\" }\n")
	writeFile(t, dir, filepath.Join("internal", "svc", "svc.go"),
		"package svc\n\nimport \"example.com/realproj/internal/api\"\n\nfunc Run() string { return api.Greet() }\n")
	return dir
}

// TestDepsCollector_Collect_RealGoModule drives DepsCollector.Collect with the
// production execCommandRunner against a real on-disk Go module, so the entire
// `go list -deps -json ./...` -> parseGoListStream -> goListNodes ->
// PackageGraph pipeline runs end-to-end. The fixture is clean (acyclic, no
// layer violations, no heavy/unused module deps), so Collect must succeed and
// emit zero signals — proving the real-toolchain parse path produces a usable,
// drift-free graph rather than a spurious finding.
func TestDepsCollector_Collect_RealGoModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := realGoModuleFixture(t)

	c := newDepsCollector(execCommandRunner, defaultLayerRules, heavyDepFanIn)
	sigs, err := c.Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Collect over real module: %v", err)
	}
	if len(sigs) != 0 {
		t.Fatalf("clean real module must yield no signals, got %+v", sigs)
	}
}

// TestDepsCollector_loadPackageGraph_RealGoModule parses the same real module's
// `go list` output directly through loadPackageGraph (the keystone Collect
// delegates to) and asserts the two internal packages and their import edge are
// recovered, with the stdlib dropped and the svc->api edge captured as an
// internal edge. This pins the real-toolchain parse to a concrete graph shape.
func TestDepsCollector_loadPackageGraph_RealGoModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := realGoModuleFixture(t)
	const modulePrefix = "example.com/realproj"

	graph, err := loadPackageGraph(context.Background(), execCommandRunner, dir, modulePrefix)
	if err != nil {
		t.Fatalf("loadPackageGraph over real module: %v", err)
	}

	nodes := graph.Nodes()
	wantNodes := map[string]bool{
		modulePrefix + "/internal/api": false,
		modulePrefix + "/internal/svc": false,
	}
	for _, n := range nodes {
		if _, ok := wantNodes[n]; ok {
			wantNodes[n] = true
		}
	}
	for n, seen := range wantNodes {
		if !seen {
			t.Fatalf("internal package %q missing from graph nodes %v", n, nodes)
		}
	}

	// The real module is acyclic, so the SCC pass finds no multi-package cycle.
	for _, comp := range graph.StronglyConnected() {
		if len(comp) > 1 {
			t.Fatalf("acyclic real module must yield no import cycle, got SCC %v", comp)
		}
	}

	// svc depends on api, so api's blast radius includes svc.
	blast := graph.BlastRadius(modulePrefix + "/internal/api")
	var sawSvc bool
	for _, dep := range blast {
		if dep == modulePrefix+"/internal/svc" {
			sawSvc = true
		}
	}
	if !sawSvc {
		t.Fatalf("api blast radius must include svc, got %v", blast)
	}
}
