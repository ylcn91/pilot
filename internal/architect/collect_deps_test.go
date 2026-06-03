package architect

import (
	"context"
	"errors"
	"testing"
)

// depFixtureDir writes a minimal go.mod so modulePathFromDir/requiredModules
// resolve, returning the temp dir. The module path matches the go-list fixture.
func depFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gomod := `module example.com/proj

go 1.24

require (
	github.com/dead/dep v1.0.0
	github.com/live/dep v1.0.0
)
`
	writeFile(t, dir, "go.mod", gomod)
	return dir
}

// cyclicGoList is a go-list stream with a planted a<->b cycle and a planted
// executor->config layer violation, used to prove the collector emits the
// matching signal kinds.
const cyclicGoList = `
{"ImportPath":"example.com/proj/internal/a","Module":{"Path":"example.com/proj"},"Imports":["example.com/proj/internal/b"]}
{"ImportPath":"example.com/proj/internal/b","Module":{"Path":"example.com/proj"},"Imports":["example.com/proj/internal/a"]}
{"ImportPath":"example.com/proj/internal/executor","Module":{"Path":"example.com/proj"},"Imports":["example.com/proj/internal/config"]}
{"ImportPath":"example.com/proj/internal/config","Module":{"Path":"example.com/proj"},"Imports":[]}
`

func modGraphFixture() []byte {
	// github.com/live/dep has fan-in 9 (heavy, threshold 8); dead/dep fan-in 1.
	var b []byte
	b = append(b, []byte("example.com/proj github.com/live/dep@v1.0.0\n")...)
	b = append(b, []byte("example.com/proj github.com/dead/dep@v1.0.0\n")...)
	for i := 0; i < 8; i++ {
		line := "example.com/r" + string(rune('a'+i)) + "@v1 github.com/live/dep@v1.0.0\n"
		b = append(b, []byte(line)...)
	}
	return b
}

func depCollectorRunner() *fakeRunner {
	unused := []byte("# github.com/dead/dep\n(main module does not need package github.com/dead/dep)\n")
	used := []byte("# github.com/live/dep\nexample.com/proj\ngithub.com/live/dep\n")
	return newFakeRunner().
		on([]byte(cyclicGoList), nil, "go", "list", "-deps", "-json", "./...").
		on(modGraphFixture(), nil, "go", "mod", "graph").
		on(unused, nil, "go", "mod", "why", "-m", "github.com/dead/dep").
		on(used, nil, "go", "mod", "why", "-m", "github.com/live/dep")
}

func signalsByKind(sigs []Signal) map[string][]Signal {
	out := map[string][]Signal{}
	for _, s := range sigs {
		out[s.Kind] = append(out[s.Kind], s)
	}
	return out
}

func TestDepsCollector_EmitsAllSignalKinds(t *testing.T) {
	dir := depFixtureDir(t)
	c := newDepsCollector(depCollectorRunner().run, defaultLayerRules, heavyDepFanIn)

	sigs, err := c.Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	byKind := signalsByKind(sigs)

	if len(byKind[kindImportCycle]) != 1 {
		t.Errorf("want 1 import_cycle_risk, got %d", len(byKind[kindImportCycle]))
	}
	if len(byKind[kindLayerViolation]) != 1 {
		t.Errorf("want 1 layer_violation, got %d: %+v", len(byKind[kindLayerViolation]), byKind[kindLayerViolation])
	}
	if len(byKind[kindHeavyDep]) != 1 {
		t.Errorf("want 1 heavy_dep, got %d: %+v", len(byKind[kindHeavyDep]), byKind[kindHeavyDep])
	}
	if len(byKind[kindUnusedDep]) != 1 {
		t.Errorf("want 1 unused_dep, got %d: %+v", len(byKind[kindUnusedDep]), byKind[kindUnusedDep])
	}

	if v := byKind[kindLayerViolation][0]; v.Risk != "high" {
		t.Errorf("layer violation risk = %q, want high", v.Risk)
	}
	if h := byKind[kindHeavyDep][0]; h.File != "github.com/live/dep" {
		t.Errorf("heavy dep = %q, want live/dep", h.File)
	}
	if u := byKind[kindUnusedDep][0]; u.File != "github.com/dead/dep" {
		t.Errorf("unused dep = %q, want dead/dep", u.File)
	}
}

func TestDepsCollector_Name(t *testing.T) {
	if got := NewDepsCollector().Name(); got != "dependency_doctor" {
		t.Errorf("Name() = %q", got)
	}
}

// TestDepsCollector_DegradesWhenGoListFails proves a total `go list` failure
// surfaces as a collector error (the Scanner then skips it), not a panic.
func TestDepsCollector_DegradesWhenGoListFails(t *testing.T) {
	dir := depFixtureDir(t)
	r := newFakeRunner().on(nil, errors.New("go list exploded"), "go", "list", "-deps", "-json", "./...")
	c := newDepsCollector(r.run, defaultLayerRules, heavyDepFanIn)

	sigs, err := c.Collect(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error when go list fails entirely")
	}
	if sigs != nil {
		t.Errorf("expected nil signals on graph build failure, got %+v", sigs)
	}
}

// TestDepsCollector_HeavyAndUnusedDegradeIndependently proves a failing
// `go mod graph` does not suppress cycle/layer signals from the graph that did
// load.
func TestDepsCollector_SubAnalysesAreIndependent(t *testing.T) {
	dir := depFixtureDir(t)
	r := newFakeRunner().
		on([]byte(cyclicGoList), nil, "go", "list", "-deps", "-json", "./...").
		on(nil, errors.New("no graph"), "go", "mod", "graph").
		on([]byte("# github.com/dead/dep\nexample.com/proj\n"), nil, "go", "mod", "why", "-m", "github.com/dead/dep").
		on([]byte("# github.com/live/dep\nexample.com/proj\n"), nil, "go", "mod", "why", "-m", "github.com/live/dep")
	c := newDepsCollector(r.run, defaultLayerRules, heavyDepFanIn)

	sigs, err := c.Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	byKind := signalsByKind(sigs)
	if len(byKind[kindImportCycle]) != 1 {
		t.Errorf("cycle signal must survive a mod-graph failure, got %d", len(byKind[kindImportCycle]))
	}
	if len(byKind[kindHeavyDep]) != 0 {
		t.Errorf("failed mod graph must yield no heavy deps, got %d", len(byKind[kindHeavyDep]))
	}
}

// TestDepsCollector_CleanGraphNoSignals proves a healthy project (no cycles, no
// violations, all deps used, none heavy) yields zero signals — the silent,
// happy path.
func TestDepsCollector_CleanGraphNoSignals(t *testing.T) {
	dir := depFixtureDir(t)
	cleanList := `
{"ImportPath":"example.com/proj/internal/api","Module":{"Path":"example.com/proj"},"Imports":[]}
{"ImportPath":"example.com/proj/internal/svc","Module":{"Path":"example.com/proj"},"Imports":["example.com/proj/internal/api"]}
`
	used := func(mod string) []byte {
		return []byte("# " + mod + "\nexample.com/proj\n" + mod + "\n")
	}
	r := newFakeRunner().
		on([]byte(cleanList), nil, "go", "list", "-deps", "-json", "./...").
		on([]byte("example.com/proj github.com/dead/dep@v1\n"), nil, "go", "mod", "graph").
		on(used("github.com/dead/dep"), nil, "go", "mod", "why", "-m", "github.com/dead/dep").
		on(used("github.com/live/dep"), nil, "go", "mod", "why", "-m", "github.com/live/dep")
	c := newDepsCollector(r.run, defaultLayerRules, heavyDepFanIn)

	sigs, err := c.Collect(context.Background(), dir)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(sigs) != 0 {
		t.Fatalf("clean project must yield no signals, got %+v", sigs)
	}
}

func TestJoinList_Truncates(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	got := joinList(items)
	if got != "a, b, c, d, e, f, +2 more" {
		t.Errorf("joinList = %q", got)
	}
	if short := joinList([]string{"x", "y"}); short != "x, y" {
		t.Errorf("short joinList = %q", short)
	}
}
