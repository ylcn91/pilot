package architect

import (
	"strings"
	"testing"
)

func TestLensByName_CoreIsDefault(t *testing.T) {
	// Empty name resolves to the core lens (backwards-compatible no-flag path).
	l, err := LensByName("")
	if err != nil {
		t.Fatalf("empty name should resolve to core: %v", err)
	}
	if l.Name != CoreLensName {
		t.Fatalf("empty name => %q, want %q", l.Name, CoreLensName)
	}
}

func TestLensByName_CaseInsensitive(t *testing.T) {
	l, err := LensByName("  DepDoctor  ")
	if err != nil {
		t.Fatalf("case/space-insensitive lookup failed: %v", err)
	}
	if l.Name != DepDoctorLensName {
		t.Fatalf("got %q, want %q", l.Name, DepDoctorLensName)
	}
}

func TestLensByName_Unknown(t *testing.T) {
	_, err := LensByName("nope")
	if err == nil {
		t.Fatal("unknown lens must error")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error should list available lenses, got %q", err.Error())
	}
}

func TestLensNames_CoreFirst(t *testing.T) {
	all := LensNames()
	if len(all) == 0 || all[0] != CoreLensName {
		t.Fatalf("LensNames must lead with core, got %v", all)
	}
	if !contains(all, DepDoctorLensName) {
		t.Errorf("depdoctor must be registered, got %v", all)
	}
}

func TestCoreLens_BundlesCoreCollectors(t *testing.T) {
	l, _ := LensByName(CoreLensName)
	got := l.Collectors("/proj", ScanOptions{})
	// Core roster (no coverage): loc, todo, duplicate_block, lint.
	wantNames := map[string]bool{
		"loc_over_400":     true,
		"todo_fixme":       true,
		kindDuplicateBlock: true,
		"lint":             true,
	}
	if len(got) != len(wantNames) {
		t.Fatalf("core lens collectors = %d, want %d: %v", len(got), len(wantNames), names(got))
	}
	for _, c := range got {
		if !wantNames[c.Name()] {
			t.Errorf("unexpected core collector %q", c.Name())
		}
	}
}

func TestCoreLens_CoverageWhenRequested(t *testing.T) {
	l, _ := LensByName(CoreLensName)
	got := l.Collectors("/proj", ScanOptions{MinCoverage: 80})
	if !contains(names(got), "coverage") {
		t.Errorf("MinCoverage>0 must add the coverage collector, got %v", names(got))
	}
}

func TestDepDoctorLens_BundlesDepsCollector(t *testing.T) {
	l, err := LensByName(DepDoctorLensName)
	if err != nil {
		t.Fatalf("depdoctor lens missing: %v", err)
	}
	got := l.Collectors("/proj", ScanOptions{})
	if len(got) != 1 || got[0].Name() != "dependency_doctor" {
		t.Fatalf("depdoctor lens collectors = %v, want [dependency_doctor]", names(got))
	}
}

func TestRadarLens_BundlesChurnCollector(t *testing.T) {
	l, err := LensByName(RadarLensName)
	if err != nil {
		t.Fatalf("radar lens missing: %v", err)
	}
	src := &mockFailureSource{}
	got := l.Collectors("/proj", ScanOptions{FailureSource: src})
	var churn *ChurnCollector
	for _, c := range got {
		if ch, ok := c.(*ChurnCollector); ok {
			churn = ch
		}
	}
	if churn == nil {
		t.Fatalf("radar lens must include a ChurnCollector, got %v", names(got))
	}
	if churn.Name() != "churn_hotspot" {
		t.Errorf("collector Name = %q, want churn_hotspot", churn.Name())
	}
	if churn.source != src {
		t.Error("ChurnCollector must receive the opts.FailureSource")
	}
}

func TestRadarLens_ChurnInertWithoutSource(t *testing.T) {
	l, _ := LensByName(RadarLensName)
	got := l.Collectors("/proj", ScanOptions{})
	var churn *ChurnCollector
	for _, c := range got {
		if ch, ok := c.(*ChurnCollector); ok {
			churn = ch
		}
	}
	if churn == nil {
		t.Fatalf("radar lens roster must always include a ChurnCollector, got %v", names(got))
	}
	if churn.source != nil {
		t.Error("nil FailureSource must leave the ChurnCollector inert")
	}
}

func TestBuildLensScanner_Core(t *testing.T) {
	s, err := BuildLensScanner("", "/proj", ScanOptions{})
	if err != nil {
		t.Fatalf("build core scanner: %v", err)
	}
	if len(s.Collectors()) != 4 {
		t.Fatalf("core scanner should have 4 collectors, got %d", len(s.Collectors()))
	}
}

func TestBuildLensScanner_DepDoctor(t *testing.T) {
	s, err := BuildLensScanner("depdoctor", "/proj", ScanOptions{})
	if err != nil {
		t.Fatalf("build depdoctor scanner: %v", err)
	}
	if len(s.Collectors()) != 1 || s.Collectors()[0].Name() != "dependency_doctor" {
		t.Fatalf("depdoctor scanner = %v", names(s.Collectors()))
	}
}

func TestBuildLensScanner_UnknownLensErrors(t *testing.T) {
	if _, err := BuildLensScanner("ghost", "/proj", ScanOptions{}); err == nil {
		t.Fatal("unknown lens must error")
	}
}

// TestBuildLensScanner_SignalsFilterApplies proves the per-Kind Signals filter
// works across lenses (here narrowing the core roster to just the LOC
// collector).
func TestBuildLensScanner_SignalsFilterApplies(t *testing.T) {
	s, err := BuildLensScanner("core", "/proj", ScanOptions{Signals: []string{"loc_over_400"}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(s.Collectors()) != 1 || s.Collectors()[0].Name() != "loc_over_400" {
		t.Fatalf("Signals filter not applied: %v", names(s.Collectors()))
	}
}

func TestRegisterLens_RejectsDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate lens registration must panic")
		}
	}()
	// core is already registered.
	RegisterLens(Lens{Name: CoreLensName, Collectors: func(string, ScanOptions) []Collector { return nil }})
}

func TestRegisterLens_RejectsEmptyName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("empty lens name must panic")
		}
	}()
	RegisterLens(Lens{Name: "  ", Collectors: func(string, ScanOptions) []Collector { return nil }})
}

func TestRegisterLens_RejectsNilFactory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil Collectors factory must panic")
		}
	}()
	RegisterLens(Lens{Name: "uniquelens-nilfactory"})
}

func TestNormalizeLensName(t *testing.T) {
	if got := normalizeLensName("  FooBar "); got != "foobar" {
		t.Errorf("normalizeLensName = %q", got)
	}
}

func names(cs []Collector) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name()
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
