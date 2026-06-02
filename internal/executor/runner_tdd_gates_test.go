package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scriptedGateChecker returns a pre-programmed QualityOutcome on each Check call,
// advancing through outcomes; the last outcome repeats once exhausted.
type scriptedGateChecker struct {
	outcomes []*QualityOutcome
	calls    *int
}

func (s *scriptedGateChecker) Check(ctx context.Context) (*QualityOutcome, error) {
	i := *s.calls
	*s.calls++
	if i >= len(s.outcomes) {
		i = len(s.outcomes) - 1
	}
	return s.outcomes[i], nil
}

// scriptedFactory builds a TDDGateCheckerFactory whose checkers walk a shared
// outcome script. checkCalls/cmds capture how the gate invoked the factory.
func scriptedFactory(outcomes []*QualityOutcome, checkCalls *int, cmds *[]string) TDDGateCheckerFactory {
	return func(taskID, projectPath, gateCommand string) QualityChecker {
		*cmds = append(*cmds, gateCommand)
		return &scriptedGateChecker{outcomes: outcomes, calls: checkCalls}
	}
}

func pass() *QualityOutcome  { return &QualityOutcome{Passed: true} }
func fail2() *QualityOutcome { return &QualityOutcome{Passed: false, RetryFeedback: "boom"} }

// scriptedGoTestRunner adapts a QualityOutcome script to the per-test go-test
// runner seam so Go-project gates (which bypass the exit-code QualityChecker) can
// be driven deterministically. For each NAMED-test call it consumes one scripted
// outcome and maps it to per-test results: a passed outcome => every named test
// "pass"; a failed outcome => every named test "fail" (with RetryFeedback as the
// raw). The baseline call (nil testNames) always returns an empty green run and
// does NOT advance the script. It shares calls/cmds with the scripted factory so
// existing assertions on invocation count and scoped commands still hold.
func scriptedGoTestRunner(outcomes []*QualityOutcome, calls *int, cmds *[]string) goTestRunnerFunc {
	return func(_ context.Context, _ string, testNames []string, _ time.Duration) (*goTestRun, error) {
		if len(testNames) == 0 {
			// Baseline / whole-suite probe: empty green suite.
			return &goTestRun{Results: map[string]goTestResult{}}, nil
		}
		*cmds = append(*cmds, "go test -json -count=1 -run '^("+strings.Join(testNames, "|")+")$' ./...")
		i := *calls
		*calls++
		if i >= len(outcomes) {
			i = len(outcomes) - 1
		}
		oc := outcomes[i]
		action := "fail"
		if oc.Passed {
			action = "pass"
		}
		results := make(map[string]goTestResult, len(testNames))
		for _, n := range testNames {
			results[n] = goTestResult{Action: action}
		}
		return &goTestRun{Results: results, Raw: oc.RetryFeedback}, nil
	}
}

func TestEnforceTDDRedGate(t *testing.T) {
	tests := []struct {
		name      string
		outcomes  []*QualityOutcome // per Check call
		roleMax   int
		wantErr   string // substring; "" => no error
		wantReran int    // expected test-author reruns
	}{
		{
			name:      "tests already red on first check",
			outcomes:  []*QualityOutcome{fail2()},
			roleMax:   1,
			wantReran: 0,
		},
		{
			name:      "not red then red after one role retry",
			outcomes:  []*QualityOutcome{pass(), fail2()},
			roleMax:   1,
			wantReran: 1,
		},
		{
			name:      "still not red after role retry aborts",
			outcomes:  []*QualityOutcome{pass(), pass()},
			roleMax:   1,
			wantErr:   reasonTDDRedGateNotRed,
			wantReran: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRunner()
			proj := goProjectDir(t)
			var checkCalls int
			var cmds []string
			r.SetTDDGateCheckerFactory(scriptedFactory(tt.outcomes, &checkCalls, &cmds))
			r.tddGoTestRunner = scriptedGoTestRunner(tt.outcomes, &checkCalls, &cmds)

			reran := 0
			rerun := func(ctx context.Context, feedback string) error { reran++; return nil }

			err := r.enforceTDDRedGate(context.Background(), "t1", proj, []string{"TestX"}, true, tt.roleMax, rerun)
			assertErr(t, err, tt.wantErr)
			if reran != tt.wantReran {
				t.Errorf("test-author reruns = %d, want %d", reran, tt.wantReran)
			}
		})
	}
}

func TestEnforceTDDGreenGate(t *testing.T) {
	tests := []struct {
		name      string
		outcomes  []*QualityOutcome
		greenMax  int
		wantErr   string
		wantReran int
	}{
		{
			name:      "green on first check",
			outcomes:  []*QualityOutcome{pass()},
			greenMax:  2,
			wantReran: 0,
		},
		{
			name:      "fails then passes after one implementer retry",
			outcomes:  []*QualityOutcome{fail2(), pass()},
			greenMax:  2,
			wantReran: 1,
		},
		{
			name:      "fails through exhaustion",
			outcomes:  []*QualityOutcome{fail2(), fail2(), fail2()},
			greenMax:  2,
			wantErr:   reasonTDDGreenGateFailed,
			wantReran: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRunner()
			proj := goProjectDir(t)
			var checkCalls int
			var cmds []string
			r.SetTDDGateCheckerFactory(scriptedFactory(tt.outcomes, &checkCalls, &cmds))
			r.tddGoTestRunner = scriptedGoTestRunner(tt.outcomes, &checkCalls, &cmds)

			reran := 0
			var lastFeedback string
			rerun := func(ctx context.Context, feedback string) error {
				reran++
				lastFeedback = feedback
				return nil
			}

			err := r.enforceTDDGreenGate(context.Background(), "t1", proj, []string{"TestX"}, true, tt.greenMax, rerun)
			assertErr(t, err, tt.wantErr)
			if reran != tt.wantReran {
				t.Errorf("implementer reruns = %d, want %d", reran, tt.wantReran)
			}
			// The GREEN gate's per-test verdict feedback names the unsatisfied test.
			if tt.wantReran > 0 && !strings.Contains(lastFeedback, "TestX") {
				t.Errorf("implementer feedback = %q, want failing test fed back", lastFeedback)
			}
		})
	}
}

func TestTDDGateCommandScoping(t *testing.T) {
	r := NewRunner()
	proj := goProjectDir(t)
	// Go project => scoped run command.
	got := r.tddGateCommand(proj, []string{"TestA", "TestB"}, true)
	want := "go test -run '^(TestA|TestB)$' ./..."
	if got != want {
		t.Errorf("scoped Go command = %q, want %q", got, want)
	}

	// scopeToNew=false => whole-suite command (not the -run form).
	whole := r.tddGateCommand(proj, []string{"TestA"}, false)
	if strings.Contains(whole, "-run") {
		t.Errorf("unscoped command should not include -run: %q", whole)
	}

	// No test names => fall back to whole-suite even when scoping is on.
	noNames := r.tddGateCommand(proj, nil, true)
	if strings.Contains(noNames, "-run") {
		t.Errorf("command with no names should not include -run: %q", noNames)
	}
}

// goProjectDir creates a throwaway directory containing a go.mod so the gate's
// project-type detection treats it as a Go project.
func goProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tddgatetest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

func TestParseTestsAdded(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"TestFoo, TestBar", []string{"TestFoo", "TestBar"}},
		{"TestFoo\nTestBar\nTestFoo", []string{"TestFoo", "TestBar"}}, // de-dup, preserve order
		{"  TestA   TestB  ", []string{"TestA", "TestB"}},
		{"123bad, ok_Name, has-dash", []string{"ok_Name"}}, // drop invalid identifiers
		{"", nil},
	}
	for _, tt := range tests {
		got := parseTestsAdded(tt.in)
		if !equalStrings(got, tt.want) {
			t.Errorf("parseTestsAdded(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func assertErr(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
