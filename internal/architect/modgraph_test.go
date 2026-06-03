package architect

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func TestParseModGraph_FanIn(t *testing.T) {
	// root requires A and B; A requires C; B requires C. So C has fan-in 2
	// (A and B), A and B each fan-in 1 (root).
	graph := `example.com/root example.com/a@v1.0.0
example.com/root example.com/b@v1.0.0
example.com/a@v1.0.0 example.com/c@v1.0.0
example.com/b@v1.0.0 example.com/c@v1.0.0
`
	fanIn := parseModGraph([]byte(graph))
	want := map[string]int{
		"example.com/a": 1,
		"example.com/b": 1,
		"example.com/c": 2,
	}
	if !reflect.DeepEqual(map[string]int(fanIn), want) {
		t.Fatalf("fanIn = %v, want %v", map[string]int(fanIn), want)
	}
}

func TestParseModGraph_DedupsRequirers(t *testing.T) {
	// Same requirer at two versions both pointing at C collapse to fan-in 1.
	graph := `example.com/a@v1.0.0 example.com/c@v1.0.0
example.com/a@v2.0.0 example.com/c@v1.0.0
`
	fanIn := parseModGraph([]byte(graph))
	if fanIn["example.com/c"] != 1 {
		t.Fatalf("versions of one requirer must collapse: fanIn[c] = %d, want 1", fanIn["example.com/c"])
	}
}

func TestParseModGraph_IgnoresMalformedLines(t *testing.T) {
	graph := "only-one-field\na@v1 b@v1\n\n   \nthree fields here\n"
	fanIn := parseModGraph([]byte(graph))
	if fanIn["b"] != 1 {
		t.Fatalf("valid line must still count: %v", map[string]int(fanIn))
	}
	if len(fanIn) != 1 {
		t.Fatalf("malformed lines must be skipped, got %v", map[string]int(fanIn))
	}
}

func TestStripVersion(t *testing.T) {
	cases := map[string]string{
		"mod@v1.2.3": "mod",
		"mod":        "mod",
		"a/b/c@v0":   "a/b/c",
	}
	for in, want := range cases {
		if got := stripVersion(in); got != want {
			t.Errorf("stripVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWhyOutputIsUnused(t *testing.T) {
	unused := "# example.com/x\n(main module does not need package example.com/x)\n"
	if !whyOutputIsUnused([]byte(unused)) {
		t.Error("should detect not-needed marker")
	}
	used := "# example.com/x\nexample.com/proj/internal/a\nexample.com/x\n"
	if whyOutputIsUnused([]byte(used)) {
		t.Error("used module must not be flagged unused")
	}
	if whyOutputIsUnused(nil) {
		t.Error("empty output is not 'unused'")
	}
}

func TestIsModuleUnused_WithFakeRunner(t *testing.T) {
	unusedOut := []byte("# m\n(main module does not need module m)\n")
	usedOut := []byte("# m\nexample.com/proj\nm\n")

	r := newFakeRunner().
		on(unusedOut, nil, "go", "mod", "why", "-m", "dead/mod").
		on(usedOut, nil, "go", "mod", "why", "-m", "live/mod")

	if !isModuleUnused(context.Background(), r.run, "/proj", "dead/mod") {
		t.Error("dead/mod should be unused")
	}
	if isModuleUnused(context.Background(), r.run, "/proj", "live/mod") {
		t.Error("live/mod should be used")
	}
}

func TestIsModuleUnused_RunErrorIsNotUnused(t *testing.T) {
	r := newFakeRunner().on(nil, os.ErrNotExist, "go", "mod", "why", "-m", "m")
	if isModuleUnused(context.Background(), r.run, "/proj", "m") {
		t.Error("a runner error must not produce a false unused positive")
	}
}

func TestParseModulePath(t *testing.T) {
	gomod := "module github.com/acme/thing\n\ngo 1.24\n"
	if got := parseModulePath([]byte(gomod)); got != "github.com/acme/thing" {
		t.Errorf("parseModulePath = %q", got)
	}
	if got := parseModulePath([]byte("go 1.24\n")); got != "" {
		t.Errorf("no module directive should yield empty, got %q", got)
	}
	quoted := "module \"github.com/acme/quoted\"\n"
	if got := parseModulePath([]byte(quoted)); got != "github.com/acme/quoted" {
		t.Errorf("quoted module path = %q", got)
	}
}

func TestParseRequiredModules_BlockAndSingle(t *testing.T) {
	gomod := `module example.com/proj

go 1.24

require github.com/single/dep v1.0.0

require (
	github.com/a/b v1.2.3 // indirect
	github.com/c/d v0.1.0
)
`
	got := parseRequiredModules([]byte(gomod))
	want := []string{"github.com/single/dep", "github.com/a/b", "github.com/c/d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required modules = %v, want %v", got, want)
	}
}

func TestModulePathFromDir_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/fromdir\n\ngo 1.24\n")
	if got := modulePathFromDir(dir); got != "example.com/fromdir" {
		t.Errorf("modulePathFromDir = %q", got)
	}
}

func TestModulePathFromDir_MissingFile(t *testing.T) {
	if got := modulePathFromDir(t.TempDir()); got != "" {
		t.Errorf("missing go.mod must yield empty, got %q", got)
	}
}

func TestRequiredModules_MissingFile(t *testing.T) {
	if got := requiredModules(t.TempDir()); got != nil {
		t.Errorf("missing go.mod must yield nil, got %v", got)
	}
}
