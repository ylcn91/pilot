package architect

import (
	"context"
	"path/filepath"
	"testing"
)

func collectorNames(s *Scanner) []string {
	cs := s.Collectors()
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name()
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestBuildDefaultScanner_DefaultRoster(t *testing.T) {
	s := BuildDefaultScanner(ScanOptions{})
	names := collectorNames(s)
	// Coverage is gated behind MinCoverage > 0, so the default roster excludes it.
	for _, want := range []string{"loc_over_400", "todo_fixme", kindDuplicateBlock, "lint"} {
		if !containsName(names, want) {
			t.Errorf("default roster missing %q; got %v", want, names)
		}
	}
	if containsName(names, "coverage") {
		t.Errorf("coverage must be excluded when MinCoverage is 0; got %v", names)
	}
}

func TestBuildDefaultScanner_CoverageEnabledByThreshold(t *testing.T) {
	s := BuildDefaultScanner(ScanOptions{MinCoverage: 80})
	if !containsName(collectorNames(s), "coverage") {
		t.Errorf("coverage collector should be present when MinCoverage > 0; got %v", collectorNames(s))
	}
}

func TestBuildDefaultScanner_SignalsFilter(t *testing.T) {
	s := BuildDefaultScanner(ScanOptions{Signals: []string{"loc_over_400"}})
	names := collectorNames(s)
	if len(names) != 1 || names[0] != "loc_over_400" {
		t.Fatalf("Signals filter should keep only loc_over_400; got %v", names)
	}
}

func TestBuildDefaultScanner_SignalsFilterIncludesCoverage(t *testing.T) {
	// Coverage is only a candidate when MinCoverage > 0; the filter then keeps it.
	s := BuildDefaultScanner(ScanOptions{MinCoverage: 50, Signals: []string{"coverage"}})
	names := collectorNames(s)
	if len(names) != 1 || names[0] != "coverage" {
		t.Fatalf("filter should keep only coverage; got %v", names)
	}
}

func TestBuildDefaultScanner_SignalsFilterIncludesDuplicateBlock(t *testing.T) {
	s := BuildDefaultScanner(ScanOptions{Signals: []string{kindDuplicateBlock}})
	names := collectorNames(s)
	if len(names) != 1 || names[0] != kindDuplicateBlock {
		t.Fatalf("filter should keep only duplicate_block; got %v", names)
	}
}

func TestBuildDefaultScanner_UnknownSignalYieldsEmptyRoster(t *testing.T) {
	s := BuildDefaultScanner(ScanOptions{Signals: []string{"does-not-exist"}})
	if got := collectorNames(s); len(got) != 0 {
		t.Fatalf("unknown signal should yield empty roster; got %v", got)
	}
}

// TestBuildDefaultScanner_ScansFixture is an end-to-end happy path: the default
// roster (sans quality runner) over a temp fixture finds the oversized file and
// the TODO without panicking on the inert lint collector.
func TestBuildDefaultScanner_ScansFixture(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "big.go", 500)
	writeFile(t, dir, filepath.Join("svc.go"), "package svc\n// TODO fix\n")
	writeDupFile(t, dir, "dup_a.go", dupBlock)
	writeDupFile(t, dir, "dup_b.go", dupBlock)

	s := BuildDefaultScanner(ScanOptions{})
	signals, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var loc, todo, duplicate int
	for _, sig := range signals {
		switch sig.Kind {
		case "loc_over_400":
			loc++
		case "todo_fixme":
			todo++
		case kindDuplicateBlock:
			duplicate++
		}
	}
	if loc != 1 {
		t.Errorf("expected 1 LOC signal, got %d", loc)
	}
	if todo != 1 {
		t.Errorf("expected 1 TODO signal, got %d", todo)
	}
	if duplicate == 0 {
		t.Errorf("expected duplicate_block signal, got none in %+v", signals)
	}
}

func TestGateRunnerFromQuality_NilStaysNil(t *testing.T) {
	if got := gateRunnerFromQuality(nil); got != nil {
		t.Errorf("nil quality runner must map to nil gateRunner, got %v", got)
	}
}

func TestFilterCollectors_EmptyWantKeepsAll(t *testing.T) {
	in := []Collector{NewLOCCollector(), NewTODOCollector()}
	out := filterCollectors(in, nil)
	if len(out) != 2 {
		t.Fatalf("empty want must keep all; got %d", len(out))
	}
}
