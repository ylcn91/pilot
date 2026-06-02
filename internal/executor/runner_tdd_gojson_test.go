package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunGoTestJSONRealToolchain exercises the real `go test -json -count=1`
// runner against an on-disk module with a failing, passing, and skipped test,
// proving the integration end-to-end (not just the seam).
func TestRunGoTestJSONRealToolchain(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module realtdd\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "x_test.go"), `package realtdd

import "testing"

func TestRedFail(t *testing.T)  { t.Fatal("intentional") }
func TestRedPass(t *testing.T)  {}
func TestRedSkip(t *testing.T)  { t.Skip("skip") }
`)
	run, err := runGoTestJSON(context.Background(), dir, []string{"TestRedFail", "TestRedPass", "TestRedSkip"}, 2*time.Minute)
	if err != nil {
		t.Fatalf("runGoTestJSON: %v", err)
	}
	if run.CompileFailed {
		t.Fatalf("unexpected compile failure: %s", run.Raw)
	}
	want := map[string]string{"TestRedFail": "fail", "TestRedPass": "pass", "TestRedSkip": "skip"}
	for name, action := range want {
		if got := run.Results[name].Action; got != action {
			t.Errorf("%s = %q, want %q (results=%v)", name, got, action, run.Results)
		}
	}

	// RED verdict: not all named tests fail (one passes, one skips) => not proven.
	if v := evalGoTestVerdict(run, []string{"TestRedFail", "TestRedPass"}, phaseRed); v.OK {
		t.Error("RED verdict must fail when a named test passes")
	}
	// GREEN verdict over the single passing test => proven.
	if v := evalGoTestVerdict(run, []string{"TestRedPass"}, phaseGreen); !v.OK {
		t.Errorf("GREEN verdict over passing test should be OK: %q", v.Feedback)
	}
}

// TestRunGoTestJSONCompileError proves a non-compiling package yields CompileFailed
// (a legitimate RED signal), not a hard error.
func TestRunGoTestJSONCompileError(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module brokentdd\n\ngo 1.22\n")
	// References an undefined symbol => the test package will not build.
	mustWrite(t, filepath.Join(dir, "y_test.go"), `package brokentdd

import "testing"

func TestNeedsImpl(t *testing.T) { _ = Add(1, 2) }
`)
	run, err := runGoTestJSON(context.Background(), dir, []string{"TestNeedsImpl"}, 2*time.Minute)
	if err != nil {
		t.Fatalf("runGoTestJSON (compile error must not be a hard error): %v", err)
	}
	if !run.CompileFailed {
		t.Fatalf("expected CompileFailed, got results=%v raw=%s", run.Results, run.Raw)
	}
	// A compile failure is a valid RED (behavior unimplemented).
	if v := evalGoTestVerdict(run, []string{"TestNeedsImpl"}, phaseRed); !v.OK {
		t.Errorf("compile failure should satisfy RED: %q", v.Feedback)
	}
}

// TestRunGoTestJSONEmptyModule proves an empty module ("matched no packages") is
// a benign green/empty run, not a spurious compile failure.
func TestRunGoTestJSONEmptyModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module emptytdd\n\ngo 1.22\n")
	run, err := runGoTestJSON(context.Background(), dir, nil, 2*time.Minute)
	if err != nil {
		t.Fatalf("runGoTestJSON empty module: %v", err)
	}
	if run.CompileFailed {
		t.Errorf("empty module must NOT be CompileFailed: raw=%s", run.Raw)
	}
	if len(run.Results) != 0 {
		t.Errorf("empty module results = %v, want none", run.Results)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestGoTestJSONCommandCount1 asserts -count=1 is always present (defeats the Go
// test cache) and that -json is set, regardless of scoping.
func TestGoTestJSONCommandCount1(t *testing.T) {
	for _, names := range [][]string{nil, {"TestA"}, {"TestA", "TestB"}} {
		args := goTestJSONCommand(names)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-count=1") {
			t.Errorf("command %q missing -count=1", joined)
		}
		if !strings.Contains(joined, "-json") {
			t.Errorf("command %q missing -json", joined)
		}
		if args[len(args)-1] != "./..." {
			t.Errorf("command %q must target ./...", joined)
		}
	}
}

// TestGoTestJSONCommandScopedRun asserts the -run anchored alternation is built
// from the test names, and omitted entirely when there are no names.
func TestGoTestJSONCommandScopedRun(t *testing.T) {
	args := goTestJSONCommand([]string{"TestA", "TestB"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-run ^(TestA|TestB)$") {
		t.Errorf("scoped command %q missing anchored -run alternation", joined)
	}

	none := strings.Join(goTestJSONCommand(nil), " ")
	if strings.Contains(none, "-run") {
		t.Errorf("unscoped command %q must not include -run", none)
	}
}

func TestGoTestRunExpr(t *testing.T) {
	if got := goTestRunExpr(nil); got != "" {
		t.Errorf("goTestRunExpr(nil) = %q, want empty", got)
	}
	if got := goTestRunExpr([]string{"TestX"}); got != "^(TestX)$" {
		t.Errorf("goTestRunExpr single = %q", got)
	}
	if got := goTestRunExpr([]string{"TestX", "TestY"}); got != "^(TestX|TestY)$" {
		t.Errorf("goTestRunExpr multi = %q", got)
	}
}

// goJSONLine renders a single go test -json event line for fixtures.
func goJSONLine(action, test string) string {
	if test == "" {
		return `{"Action":"` + action + `"}`
	}
	return `{"Action":"` + action + `","Test":"` + test + `"}`
}

// TestParseGoTestJSONPassFailSkip covers the three terminal actions across
// multiple tests, plus the run->pass progression (last action wins) and that
// package-level events without a Test field are ignored.
func TestParseGoTestJSONPassFailSkip(t *testing.T) {
	stream := strings.Join([]string{
		goJSONLine("run", "TestPass"),
		goJSONLine("output", "TestPass"),
		goJSONLine("pass", "TestPass"),
		goJSONLine("run", "TestFail"),
		goJSONLine("fail", "TestFail"),
		goJSONLine("skip", "TestSkip"),
		goJSONLine("pass", ""), // package-level pass: no Test => ignored
	}, "\n")

	run := parseGoTestJSON([]byte(stream))
	if run.CompileFailed {
		t.Fatal("parseGoTestJSON must not set CompileFailed")
	}
	want := map[string]string{"TestPass": "pass", "TestFail": "fail", "TestSkip": "skip"}
	if len(run.Results) != len(want) {
		t.Fatalf("results = %v, want %d entries", run.Results, len(want))
	}
	for name, action := range want {
		if got := run.Results[name].Action; got != action {
			t.Errorf("%s action = %q, want %q", name, got, action)
		}
	}
}

// TestParseGoTestJSONLastActionWins ensures a test that emits both a fail and a
// later pass (e.g. retried subtests) records the LAST terminal action.
func TestParseGoTestJSONLastActionWins(t *testing.T) {
	stream := strings.Join([]string{
		goJSONLine("fail", "TestFlaky"),
		goJSONLine("pass", "TestFlaky"),
	}, "\n")
	run := parseGoTestJSON([]byte(stream))
	if got := run.Results["TestFlaky"].Action; got != "pass" {
		t.Errorf("TestFlaky action = %q, want last-wins pass", got)
	}
}

// TestParseGoTestJSONIgnoresGarbage ensures non-JSON lines (a leading build error
// banner, blank lines, plain text) are skipped without poisoning the map.
func TestParseGoTestJSONIgnoresGarbage(t *testing.T) {
	stream := strings.Join([]string{
		"# tddtest [build failed]",
		"",
		"./add_test.go:5:2: undefined: Add",
		"not-json-at-all",
		goJSONLine("fail", "TestAdd"),
	}, "\n")
	run := parseGoTestJSON([]byte(stream))
	if len(run.Results) != 1 || run.Results["TestAdd"].Action != "fail" {
		t.Fatalf("results = %v, want only TestAdd=fail", run.Results)
	}
	if run.Raw == "" {
		t.Error("Raw should retain the original output")
	}
}

// TestParseGoTestJSONEmpty covers an empty stream: no results, not compile-failed
// (the caller decides compile-failed from exit + emptiness).
func TestParseGoTestJSONEmpty(t *testing.T) {
	run := parseGoTestJSON([]byte(""))
	if len(run.Results) != 0 {
		t.Errorf("empty stream results = %v, want none", run.Results)
	}
	if run.CompileFailed {
		t.Error("empty stream must not preset CompileFailed")
	}
}

func TestIsNoPackagesOutput(t *testing.T) {
	cases := map[string]bool{
		`go: warning: "./..." matched no packages` + "\nno packages to test": true,
		"matched no packages": true,
		"no packages to test": true,
		"# tddtest [build failed]\n./x_test.go:1: undefined: Foo": false,
		"": false,
	}
	for in, want := range cases {
		if got := isNoPackagesOutput(in); got != want {
			t.Errorf("isNoPackagesOutput(%q) = %v, want %v", in, got, want)
		}
	}
}
