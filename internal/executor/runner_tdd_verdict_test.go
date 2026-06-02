package executor

import (
	"strings"
	"testing"
)

// runWith builds a goTestRun from a name->action map (no compile failure).
func runWith(results map[string]string) *goTestRun {
	r := &goTestRun{Results: map[string]goTestResult{}}
	for name, action := range results {
		r.Results[name] = goTestResult{Action: action}
	}
	return r
}

func TestEvalGoTestVerdictRed(t *testing.T) {
	tests := []struct {
		name    string
		run     *goTestRun
		names   []string
		wantOK  bool
		wantSub string // feedback substring when not OK
	}{
		{
			name:   "all named tests fail => RED proven",
			run:    runWith(map[string]string{"TestA": "fail", "TestB": "fail"}),
			names:  []string{"TestA", "TestB"},
			wantOK: true,
		},
		{
			name:    "a named test passes => RED not proven",
			run:     runWith(map[string]string{"TestA": "fail", "TestB": "pass"}),
			names:   []string{"TestA", "TestB"},
			wantOK:  false,
			wantSub: "TestB=pass",
		},
		{
			name:    "a named test is skipped => RED not proven (skip proves nothing)",
			run:     runWith(map[string]string{"TestA": "skip"}),
			names:   []string{"TestA"},
			wantOK:  false,
			wantSub: "TestA=skip",
		},
		{
			name:    "a named test is missing (typo / not added) => RED not proven",
			run:     runWith(map[string]string{"TestA": "fail"}),
			names:   []string{"TestA", "TestTypo"},
			wantOK:  false,
			wantSub: "Missing",
		},
		{
			name:   "compile failure (no results) => RED proven (behavior unimplemented)",
			run:    &goTestRun{Results: map[string]goTestResult{}, CompileFailed: true},
			names:  []string{"TestA"},
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := evalGoTestVerdict(tt.run, tt.names, phaseRed)
			if v.OK != tt.wantOK {
				t.Fatalf("OK = %v, want %v (feedback=%q)", v.OK, tt.wantOK, v.Feedback)
			}
			if !tt.wantOK {
				if v.Feedback == "" {
					t.Fatal("expected feedback when verdict not OK")
				}
				if tt.wantSub != "" && !strings.Contains(v.Feedback, tt.wantSub) {
					t.Errorf("feedback %q missing %q", v.Feedback, tt.wantSub)
				}
			}
		})
	}
}

func TestEvalGoTestVerdictGreen(t *testing.T) {
	tests := []struct {
		name    string
		run     *goTestRun
		names   []string
		wantOK  bool
		wantSub string
	}{
		{
			name:   "all named tests pass => GREEN proven",
			run:    runWith(map[string]string{"TestA": "pass", "TestB": "pass"}),
			names:  []string{"TestA", "TestB"},
			wantOK: true,
		},
		{
			name:    "a named test still fails => GREEN not proven",
			run:     runWith(map[string]string{"TestA": "pass", "TestB": "fail"}),
			names:   []string{"TestA", "TestB"},
			wantOK:  false,
			wantSub: "TestB=fail",
		},
		{
			name:    "a named test skipped => GREEN not proven",
			run:     runWith(map[string]string{"TestA": "skip"}),
			names:   []string{"TestA"},
			wantOK:  false,
			wantSub: "TestA=skip",
		},
		{
			name:    "a named test missing => GREEN not proven",
			run:     runWith(map[string]string{"TestA": "pass"}),
			names:   []string{"TestA", "TestGone"},
			wantOK:  false,
			wantSub: "Missing",
		},
		{
			name:    "compile failure => GREEN not proven (impl does not build)",
			run:     &goTestRun{Results: map[string]goTestResult{}, CompileFailed: true, Raw: "undefined: Add"},
			names:   []string{"TestA"},
			wantOK:  false,
			wantSub: "failed to compile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := evalGoTestVerdict(tt.run, tt.names, phaseGreen)
			if v.OK != tt.wantOK {
				t.Fatalf("OK = %v, want %v (feedback=%q)", v.OK, tt.wantOK, v.Feedback)
			}
			if !tt.wantOK && tt.wantSub != "" && !strings.Contains(v.Feedback, tt.wantSub) {
				t.Errorf("feedback %q missing %q", v.Feedback, tt.wantSub)
			}
		})
	}
}

// TestEvalGoTestVerdictNilRun guards the nil-run edge.
func TestEvalGoTestVerdictNilRun(t *testing.T) {
	if v := evalGoTestVerdict(nil, []string{"TestA"}, phaseRed); v.OK {
		t.Error("nil run must not be OK")
	}
}

// TestPhaseString documents the phase labels used in feedback.
func TestPhaseString(t *testing.T) {
	if phaseRed.String() != "RED" || phaseGreen.String() != "GREEN" {
		t.Errorf("phase labels = %q/%q", phaseRed, phaseGreen)
	}
}

func TestDedupeSorted(t *testing.T) {
	got := dedupeSorted([]string{"TestB", "TestA", "TestB", "", "TestA"})
	want := []string{"TestA", "TestB"}
	if !equalStrings(got, want) {
		t.Errorf("dedupeSorted = %v, want %v", got, want)
	}
}
