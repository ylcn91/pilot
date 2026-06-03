package architect

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/quality"
)

func TestTestGapLens_Registered(t *testing.T) {
	l, err := LensByName(TestGapLensName)
	if err != nil {
		t.Fatalf("testgap lens must be registered: %v", err)
	}
	if l.Name != TestGapLensName {
		t.Fatalf("lens name = %q, want %q", l.Name, TestGapLensName)
	}
	if l.Slant == nil {
		t.Fatal("testgap lens must carry an analyzer slant")
	}
	if !contains(LensNames(), TestGapLensName) {
		t.Errorf("LensNames must include testgap, got %v", LensNames())
	}
}

func TestTestGapLens_CaseInsensitiveLookup(t *testing.T) {
	l, err := LensByName("  TESTGAP ")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if l.Name != TestGapLensName {
		t.Fatalf("got %q, want %q", l.Name, TestGapLensName)
	}
}

func TestTestGapLens_BundlesBugHistoryAndMissingTests(t *testing.T) {
	l, _ := LensByName(TestGapLensName)
	// No quality runner, no coverage => bug_hotspot + missing_test only.
	got := l.Collectors("/proj", ScanOptions{})
	if len(got) != 2 {
		t.Fatalf("without coverage, testgap should wire 2 collectors, got %d: %v", len(got), names(got))
	}
	want := map[string]bool{"bug_hotspot": true, "missing_test": true}
	for _, c := range got {
		if !want[c.Name()] {
			t.Errorf("unexpected collector %q", c.Name())
		}
	}
}

func TestTestGapLens_SkipsCoverageWithoutRunner(t *testing.T) {
	l, _ := LensByName(TestGapLensName)
	got := l.Collectors("/proj", ScanOptions{
		QualityRunner: nil, // gateRunnerFromQuality(nil)==nil, so coverage is skipped
		MinCoverage:   80,
	})
	if contains(names(got), "coverage") {
		t.Fatalf("coverage needs a runner AND a threshold; nil runner must skip it: %v", names(got))
	}
}

func TestTestGapLens_AddsCoverageWhenRunnerAndThreshold(t *testing.T) {
	l, _ := LensByName(TestGapLensName)
	got := l.Collectors("/proj", ScanOptions{
		QualityRunner: quality.NewRunner(nil, "/proj"), // non-nil runner
		MinCoverage:   75,
	})
	if !contains(names(got), "coverage") {
		t.Fatalf("runner + threshold must add the coverage collector, got %v", names(got))
	}
	if len(got) != 3 {
		t.Fatalf("testgap with coverage should wire 3 collectors, got %d: %v", len(got), names(got))
	}
}

func TestTestGapLens_SkipsCoverageWithoutThreshold(t *testing.T) {
	l, _ := LensByName(TestGapLensName)
	got := l.Collectors("/proj", ScanOptions{
		QualityRunner: quality.NewRunner(nil, "/proj"), // runner present, threshold 0
		MinCoverage:   0,
	})
	if contains(names(got), "coverage") {
		t.Fatalf("zero threshold must skip coverage even with a runner, got %v", names(got))
	}
}

func TestTestGapLens_WiresFailureSource(t *testing.T) {
	l, _ := LensByName(TestGapLensName)
	src := &mockFailureSource{reasons: []*memory.FailureReason{
		{Reason: "boom", Count: 3},
	}}
	got := l.Collectors("/proj", ScanOptions{FailureSource: src})
	var bug *BugHistoryCollector
	for _, c := range got {
		if b, ok := c.(*BugHistoryCollector); ok {
			bug = b
		}
	}
	if bug == nil {
		t.Fatal("testgap lens must include a BugHistoryCollector")
	}
	if bug.source != src {
		t.Fatal("BugHistoryCollector must receive the opts.FailureSource")
	}
}

func TestTestGapLens_BuildLensScanner(t *testing.T) {
	s, err := BuildLensScanner(TestGapLensName, "/proj", ScanOptions{})
	if err != nil {
		t.Fatalf("build testgap scanner: %v", err)
	}
	if len(s.Collectors()) != 2 {
		t.Fatalf("testgap scanner (no coverage) = %v, want 2", names(s.Collectors()))
	}
}

func TestTestGapSlant_Content(t *testing.T) {
	s := testGapSlant
	if s.TaskDescription == "" || s.Heading == "" || s.Intro == "" || s.ExtraInstruction == "" {
		t.Fatal("testgap slant must populate task/heading/intro/extra")
	}
	// The slant must demand the four payload pieces the L2 spec requires.
	for _, want := range []string{"MATRIX", "CRITICAL-PATH", "RACE", "E2E", "FIXTURE-REUSE", "test_plan"} {
		if !strings.Contains(s.ExtraInstruction, want) {
			t.Errorf("slant instruction missing %q", want)
		}
	}
}
