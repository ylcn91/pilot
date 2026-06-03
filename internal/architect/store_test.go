package architect

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// architectProvider mirrors gateway.ArchitectProvider so this package can assert
// — without importing internal/gateway — that *FindingsStore structurally
// satisfies the provider contract. If FindingsStore's Findings signature ever
// drifts from the gateway interface, this assignment stops compiling.
type architectProvider interface {
	Findings() []pilotapi.Finding
}

var _ architectProvider = (*FindingsStore)(nil)

func TestFindingsStore_ZeroValueEmptyNonNil(t *testing.T) {
	var s FindingsStore
	got := s.Findings()
	if got == nil {
		t.Fatal("zero-value store must return a non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("zero-value store must be empty, got %d", len(got))
	}
	if s.Len() != 0 {
		t.Fatalf("zero-value Len = %d, want 0", s.Len())
	}
}

func TestFindingsStore_SetAndFindings(t *testing.T) {
	s := NewFindingsStore()
	in := []pilotapi.Finding{
		{Title: "a", Risk: pilotapi.RiskHigh},
		{Title: "b", Risk: pilotapi.RiskLow},
	}
	s.Set(in)

	got := s.Findings()
	if len(got) != 2 {
		t.Fatalf("Findings len = %d, want 2", len(got))
	}
	if got[0].Title != "a" || got[1].Title != "b" {
		t.Fatalf("Findings content mismatch: %+v", got)
	}
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
}

func TestFindingsStore_SetCopiesInput(t *testing.T) {
	s := NewFindingsStore()
	in := []pilotapi.Finding{{Title: "orig"}}
	s.Set(in)

	// Mutating the caller's slice after Set must not affect the store.
	in[0].Title = "mutated"
	if got := s.Findings(); got[0].Title != "orig" {
		t.Fatalf("Set must defensively copy; got %q", got[0].Title)
	}
}

func TestFindingsStore_FindingsSnapshotIsolated(t *testing.T) {
	s := NewFindingsStore()
	s.Set([]pilotapi.Finding{{Title: "v1"}})

	snap := s.Findings()
	// A later Set must not retroactively mutate a previously-returned snapshot.
	s.Set([]pilotapi.Finding{{Title: "v2"}})
	if snap[0].Title != "v1" {
		t.Fatalf("snapshot leaked store state, got %q", snap[0].Title)
	}
	// And mutating the snapshot must not corrupt the store.
	snap[0].Title = "scribble"
	if got := s.Findings(); got[0].Title != "v2" {
		t.Fatalf("snapshot mutation leaked into store, got %q", got[0].Title)
	}
}

func TestFindingsStore_SetNilClears(t *testing.T) {
	s := NewFindingsStore()
	s.Set([]pilotapi.Finding{{Title: "x"}})
	s.Set(nil)
	if got := s.Findings(); got == nil || len(got) != 0 {
		t.Fatalf("Set(nil) must clear to empty non-nil, got %+v", got)
	}
}

// TestFindingsStore_ConcurrentSetFindings hammers the store from many goroutines
// to prove Set/Findings are race-free under `go test -race`.
func TestFindingsStore_ConcurrentSetFindings(t *testing.T) {
	s := NewFindingsStore()
	const writers, readers, iters = 8, 8, 200

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		go func(id int) {
			defer wg.Done()
			batch := []pilotapi.Finding{{Title: "w", Risk: pilotapi.RiskMedium}}
			for i := 0; i < iters; i++ {
				s.Set(batch)
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				for _, f := range s.Findings() {
					_ = f.Title // touch the data so -race observes the read
				}
				_ = s.Len()
			}
		}()
	}
	wg.Wait()

	if s.Len() != 1 {
		t.Fatalf("after concurrent churn, store should hold the last batch (1), got %d", s.Len())
	}
}

func TestRadarLens_RegisteredAndSelectable(t *testing.T) {
	l, err := LensByName(RadarLensName)
	if err != nil {
		t.Fatalf("radar lens must be registered: %v", err)
	}
	if l.Name != RadarLensName {
		t.Fatalf("got %q, want %q", l.Name, RadarLensName)
	}
	if !contains(LensNames(), RadarLensName) {
		t.Errorf("radar must appear in LensNames(): %v", LensNames())
	}
	// Case/space-insensitive selection, mirroring the --lens flag path.
	if _, err := LensByName("  RADAR  "); err != nil {
		t.Errorf("radar must resolve case-insensitively: %v", err)
	}
}

func TestRadarLens_BundlesExpectedCollectors(t *testing.T) {
	l, _ := LensByName(RadarLensName)
	got := names(l.Collectors(ScanOptions{}))
	// Core (loc, todo, duplicate, lint) + dependency_doctor + stale_test + churn, no coverage.
	want := []string{"loc_over_400", "todo_fixme", kindDuplicateBlock, "lint", "dependency_doctor", "stale_test", "churn_hotspot"}
	for _, w := range want {
		if !contains(got, w) {
			t.Errorf("radar roster missing %q; got %v", w, got)
		}
	}
	if contains(got, "coverage") {
		t.Errorf("radar should not include coverage without MinCoverage; got %v", got)
	}
}

func TestRadarLens_CoverageWhenRequested(t *testing.T) {
	l, _ := LensByName(RadarLensName)
	got := names(l.Collectors(ScanOptions{MinCoverage: 75}))
	if !contains(got, "coverage") {
		t.Errorf("MinCoverage>0 must add coverage to radar roster, got %v", got)
	}
}

func TestBuildLensScanner_Radar(t *testing.T) {
	s, err := BuildLensScanner(RadarLensName, "/proj", ScanOptions{})
	if err != nil {
		t.Fatalf("build radar scanner: %v", err)
	}
	// 3 core + deps + staletests + duplication + churn = 7.
	if len(s.Collectors()) != 7 {
		t.Fatalf("radar scanner collectors = %d, want 7: %v", len(s.Collectors()), names(s.Collectors()))
	}
}

func TestRunRadar_NilStore(t *testing.T) {
	if err := RunRadar(context.Background(), RadarConfig{ProjectPath: "/x"}, nil); err == nil {
		t.Fatal("nil store must error")
	}
}

func TestRunRadar_EmptyProjectPath(t *testing.T) {
	if err := RunRadar(context.Background(), RadarConfig{}, NewFindingsStore()); err == nil {
		t.Fatal("empty project path must error")
	}
}

func TestRunRadar_ContextCancelled(t *testing.T) {
	store := NewFindingsStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunRadar(ctx, RadarConfig{ProjectPath: t.TempDir()}, store)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan must surface ctx error, got %v", err)
	}
}

// TestRunRadar_BuildLensScannerError covers the error branch where RunRadar's
// call to BuildLensScanner fails: RunRadar hardcodes RadarLensName, so the only
// way the resolver can fail is if the radar lens is not registered. The test
// temporarily removes the radar lens from the package-private registry (tests in
// this package run sequentially — none call t.Parallel — so the global mutation
// is safe), forcing the "unknown lens" path, and asserts RunRadar surfaces it
// without populating the store. t.Cleanup restores the lens so later tests see
// the normal registry.
func TestRunRadar_BuildLensScannerError(t *testing.T) {
	lensMu.Lock()
	saved, existed := lensRegistry[RadarLensName]
	delete(lensRegistry, RadarLensName)
	lensMu.Unlock()
	t.Cleanup(func() {
		lensMu.Lock()
		if existed {
			lensRegistry[RadarLensName] = saved
		} else {
			delete(lensRegistry, RadarLensName)
		}
		lensMu.Unlock()
	})

	store := NewFindingsStore()
	store.Set([]pilotapi.Finding{{Title: "prior"}})

	err := RunRadar(context.Background(), RadarConfig{ProjectPath: t.TempDir()}, store)
	if err == nil {
		t.Fatal("RunRadar must surface the BuildLensScanner error when the radar lens is unregistered")
	}
	if !strings.Contains(err.Error(), "unknown lens") {
		t.Fatalf("error must come from lens resolution, got %v", err)
	}
	// The scanner was never built, so the store must keep its prior contents
	// untouched — RunRadar only clears on a successful empty scan.
	if store.Len() != 1 {
		t.Fatalf("a build error must not mutate the store, got %d findings", store.Len())
	}
}

func TestRunRadar_EmptyProjectClearsStore(t *testing.T) {
	store := NewFindingsStore()
	store.Set([]pilotapi.Finding{{Title: "stale"}})

	// A directory with no signals (no go files, no git) yields zero findings;
	// RunRadar must succeed and clear the store rather than leaving stale data.
	if err := RunRadar(context.Background(), RadarConfig{ProjectPath: t.TempDir()}, store); err != nil {
		t.Fatalf("RunRadar on empty dir: %v", err)
	}
	if store.Len() != 0 {
		t.Fatalf("empty scan must clear store, got %d findings", store.Len())
	}
}

// TestRunRadar_PopulatesStoreFromFixture builds a project fixture with a known
// drift signal (an oversized file, which the LOC collector flags deterministically
// and offline) and proves RunRadar synthesises a ranked finding into the store
// that the dashboard sink would render.
func TestRunRadar_PopulatesStoreFromFixture(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "big.go"), oversizedGoFile())

	store := NewFindingsStore()
	if err := RunRadar(context.Background(), RadarConfig{ProjectPath: root}, store); err != nil {
		t.Fatalf("RunRadar: %v", err)
	}
	got := store.Findings()
	if len(got) == 0 {
		t.Fatal("radar must populate the store with at least one finding from the oversized file")
	}
	var sawSplit bool
	for _, f := range got {
		if f.Risk == "" || !f.Risk.IsValid() {
			t.Errorf("finding has invalid risk %q: %+v", f.Risk, f)
		}
		if f.Title == "" {
			t.Errorf("finding missing title: %+v", f)
		}
		for _, file := range f.Files {
			if filepath.Base(file) == "big.go" {
				sawSplit = true
			}
		}
	}
	if !sawSplit {
		t.Fatalf("expected a finding referencing big.go, got %+v", got)
	}
}

// TestRunRadar_DuplicateBlockPopulatesStore proves the scheduler/default radar
// path surfaces the duplicate_block collector into the shared findings store.
func TestRunRadar_DuplicateBlockPopulatesStore(t *testing.T) {
	root := t.TempDir()
	writeDupFile(t, root, "dup_a.go", dupBlock)
	writeDupFile(t, root, "dup_b.go", dupBlock)

	store := NewFindingsStore()
	if err := RunRadar(context.Background(), RadarConfig{ProjectPath: root}, store); err != nil {
		t.Fatalf("RunRadar: %v", err)
	}

	for _, f := range store.Findings() {
		if strings.Contains(f.Title, "duplicated code block") {
			return
		}
	}
	t.Fatalf("radar must surface duplicate_block findings, got %+v", store.Findings())
}

// TestRunRadar_StaleTestFlaggedViaRealGit drives the full radar lens over a real
// git fixture where a test has fallen behind its source, proving the stale-test
// collector feeds the store through RunRadar end-to-end.
func TestRunRadar_StaleTestFlaggedViaRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInit(t, root)
	writeTestFile(t, filepath.Join(root, "pkg", "svc.go"), "package pkg\n")
	writeTestFile(t, filepath.Join(root, "pkg", "svc_test.go"), "package pkg\n")
	gitCommitAll(t, root, "init")

	waitForGitTick()
	writeTestFile(t, filepath.Join(root, "pkg", "svc.go"), "package pkg\n// changed\n")
	gitCommitAll(t, root, "touch source")

	store := NewFindingsStore()
	if err := RunRadar(context.Background(), RadarConfig{ProjectPath: root}, store); err != nil {
		t.Fatalf("RunRadar: %v", err)
	}
	var sawStale bool
	for _, f := range store.Findings() {
		for _, file := range f.Files {
			if filepath.Base(file) == "svc_test.go" {
				sawStale = true
			}
		}
	}
	if !sawStale {
		t.Fatalf("radar must surface the stale test in the store, got %+v", store.Findings())
	}
}

// oversizedGoFile returns the body of a Go file just over the LOC threshold so
// the LOC collector flags it deterministically.
func oversizedGoFile() string {
	var b strings.Builder
	b.WriteString("package big\n")
	for i := 0; i < LOCThreshold+10; i++ {
		b.WriteString("// padding line to push the file over the LOC threshold\n")
	}
	return b.String()
}

// waitForGitTick sleeps long enough that the next git commit lands in a later
// whole second, so commit-time (%ct) comparisons distinguish the two commits.
func waitForGitTick() {
	time.Sleep(1100 * time.Millisecond)
}
