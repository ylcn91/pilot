package architect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// goListFixture is a deterministic `go list -deps -json` stream: a sequence of
// JSON objects (not an array), mixing project packages, a stdlib package, and a
// third-party package. modulePrefix is "example.com/proj".
const goListFixture = `
{"ImportPath":"fmt","Standard":true,"Imports":["errors"]}
{"ImportPath":"example.com/proj/internal/pilotapi","Module":{"Path":"example.com/proj"},"Imports":["fmt"]}
{"ImportPath":"example.com/proj/internal/executor","Module":{"Path":"example.com/proj"},"Imports":["example.com/proj/internal/pilotapi","github.com/3p/lib","fmt"]}
{"ImportPath":"example.com/proj/internal/config","Module":{"Path":"example.com/proj"},"Imports":["example.com/proj/internal/executor"]}
{"ImportPath":"github.com/3p/lib","Module":{"Path":"github.com/3p/lib"},"Imports":["fmt"]}
`

func TestParseGoListStream_Fixture(t *testing.T) {
	pkgs, err := parseGoListStream(strings.NewReader(goListFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pkgs) != 5 {
		t.Fatalf("want 5 packages, got %d", len(pkgs))
	}
	if pkgs[0].ImportPath != "fmt" || !pkgs[0].Standard {
		t.Errorf("first pkg = %+v, want stdlib fmt", pkgs[0])
	}
	if pkgs[1].Module == nil || pkgs[1].Module.Path != "example.com/proj" {
		t.Errorf("pilotapi module = %+v", pkgs[1].Module)
	}
}

func TestParseGoListStream_Empty(t *testing.T) {
	pkgs, err := parseGoListStream(strings.NewReader(""))
	if err != nil {
		t.Fatalf("empty stream must not error: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("want 0 packages, got %d", len(pkgs))
	}
}

// TestParseGoListStream_PartialThenGarbage returns the objects decoded before a
// malformed trailing object, alongside an error.
func TestParseGoListStream_PartialThenGarbage(t *testing.T) {
	in := `{"ImportPath":"a"}` + "\n" + `{"ImportPath":"b"}` + "\n" + `{not json`
	pkgs, err := parseGoListStream(strings.NewReader(in))
	if err == nil {
		t.Fatal("malformed trailing object must produce an error")
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2 packages decoded before the error, got %d", len(pkgs))
	}
}

func TestGoListNodes_PartitionsInternalAndExternal(t *testing.T) {
	pkgs, _ := parseGoListStream(strings.NewReader(goListFixture))
	nodes := goListNodes(pkgs, "example.com/proj")

	if len(nodes) != 3 {
		t.Fatalf("want 3 internal nodes (stdlib + 3p dropped), got %d: %+v", len(nodes), nodes)
	}
	byPath := map[string]PackageNode{}
	for _, n := range nodes {
		byPath[n.ImportPath] = n
	}
	exec, ok := byPath["example.com/proj/internal/executor"]
	if !ok {
		t.Fatal("executor node missing")
	}
	if !reflect.DeepEqual(exec.Imports, []string{"example.com/proj/internal/pilotapi"}) {
		t.Errorf("executor internal imports = %v, want only pilotapi", exec.Imports)
	}
	// fmt (stdlib) and github.com/3p/lib must be external, not internal edges.
	foundLib, foundFmt := false, false
	for _, e := range exec.ExternalImports {
		if e == "github.com/3p/lib" {
			foundLib = true
		}
		if e == "fmt" {
			foundFmt = true
		}
	}
	if !foundLib || !foundFmt {
		t.Errorf("external imports = %v, want fmt and 3p/lib", exec.ExternalImports)
	}
}

func TestGoListNodes_EmptyModulePrefixYieldsNothing(t *testing.T) {
	pkgs, _ := parseGoListStream(strings.NewReader(goListFixture))
	if got := goListNodes(pkgs, ""); len(got) != 0 {
		t.Fatalf("empty module prefix must treat nothing as internal, got %d", len(got))
	}
}

func TestIsInternalPath(t *testing.T) {
	cases := []struct {
		path, prefix string
		want         bool
	}{
		{"m/internal/x", "m", true},
		{"m", "m", true},
		{"m2/internal/x", "m", false},
		{"other/pkg", "m", false},
		{"m/internal/x", "", false},
	}
	for _, c := range cases {
		if got := isInternalPath(c.path, c.prefix); got != c.want {
			t.Errorf("isInternalPath(%q,%q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}

// fakeRunner is a programmable commandRunner keyed by the joined command line.
type fakeRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	out []byte
	err error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: map[string]fakeResponse{}}
}

func (f *fakeRunner) on(out []byte, err error, name string, args ...string) *fakeRunner {
	f.responses[key(name, args)] = fakeResponse{out: out, err: err}
	return f
}

func (f *fakeRunner) run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	k := key(name, args)
	f.calls = append(f.calls, k)
	if r, ok := f.responses[k]; ok {
		return r.out, r.err
	}
	return nil, errors.New("unexpected command: " + k)
}

func key(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func TestLoadPackageGraph_FromFakeRunner(t *testing.T) {
	r := newFakeRunner().on([]byte(goListFixture), nil, "go", "list", "-deps", "-json", "./...")
	g, err := loadPackageGraph(context.Background(), r.run, "/proj", "example.com/proj")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{
		"example.com/proj/internal/config",
		"example.com/proj/internal/executor",
		"example.com/proj/internal/pilotapi",
	}
	if !reflect.DeepEqual(g.Nodes(), want) {
		t.Fatalf("graph nodes = %v, want %v", g.Nodes(), want)
	}
}

// TestLoadPackageGraph_DegradesOnRunError proves the collector's graph build is
// best-effort: when `go list` errors and emits nothing, loadPackageGraph
// surfaces the error (the Scanner then skips this collector).
func TestLoadPackageGraph_DegradesOnRunError(t *testing.T) {
	r := newFakeRunner().on(nil, errors.New("go list boom"), "go", "list", "-deps", "-json", "./...")
	_, err := loadPackageGraph(context.Background(), r.run, "/proj", "example.com/proj")
	if err == nil {
		t.Fatal("expected error when go list fails with no output")
	}
}

// TestLoadPackageGraph_PartialOutputUsable proves a non-zero `go list` exit that
// still emitted some packages yields a usable graph (best-effort), not an error.
func TestLoadPackageGraph_PartialOutputUsable(t *testing.T) {
	partial := `{"ImportPath":"example.com/proj/internal/a","Module":{"Path":"example.com/proj"}}` + "\n"
	r := newFakeRunner().on([]byte(partial), errors.New("exit 1"), "go", "list", "-deps", "-json", "./...")
	g, err := loadPackageGraph(context.Background(), r.run, "/proj", "example.com/proj")
	if err != nil {
		t.Fatalf("partial output should be usable, got error: %v", err)
	}
	if len(g.Nodes()) != 1 {
		t.Fatalf("want 1 node from partial output, got %d", len(g.Nodes()))
	}
}
