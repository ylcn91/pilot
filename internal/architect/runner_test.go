package architect

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// runFixtureProject writes a single oversized Go file so the real LOC collector
// produces at least one signal, giving Run a non-empty SCAN result to feed
// PROPOSE.
func runFixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeGoFile(t, dir, "oversized.go", 500)
	return dir
}

// newRunConfig assembles a RunConfig whose PROPOSE backend is the given mock, so
// Run executes end-to-end without a real CLI or network.
func newRunConfig(dir string, backend *mockBackend, creator IssueCreator, searcher IssueSearcher) RunConfig {
	a := NewAnalyzer(nil, executor.BackendConfig{}, dir)
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return backend, nil
	}
	return RunConfig{
		Scanner:     NewScanner(NewLOCCollector()),
		Analyzer:    a,
		ProjectPath: dir,
		Owner:       "owner",
		Repo:        "repo",
		Creator:     creator,
		Searcher:    searcher,
	}
}

const twoFindingsJSON = `[
  {"title":"Split oversized.go","kind":"refactor","risk":"high","why_it_matters":"too long","suggested_pr_pieces":["extract helpers"],"test_plan":"go test","files":["oversized.go"]},
  {"title":"Add coverage","kind":"test-gap","risk":"medium","why_it_matters":"untested","suggested_pr_pieces":["add tests"],"test_plan":"go test","files":["other.go"]}
]`

func TestRun_FullPipelineCreatesIssues(t *testing.T) {
	dir := runFixtureProject(t)
	creator := newMockCreator()
	mb := &mockBackend{output: twoFindingsJSON}
	cfg := newRunConfig(dir, mb, creator, nil)

	res, err := Run(context.Background(), cfg, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mb.calls != 1 {
		t.Errorf("PROPOSE backend should run once, got %d", mb.calls)
	}
	if len(res.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(res.Findings))
	}
	if res.Created != 2 || res.Skipped != 0 {
		t.Fatalf("created=%d skipped=%d, want 2/0", res.Created, res.Skipped)
	}
	if creator.count() != 2 {
		t.Fatalf("expected 2 issue creates, got %d", creator.count())
	}
	// Findings come back rank-ordered: high before medium.
	if res.Findings[0].Risk != pilotapi.RiskHigh {
		t.Errorf("findings[0] risk = %s, want high (ranked)", res.Findings[0].Risk)
	}
}

func TestRun_DryRunReturnsFindingsCreatesNothing(t *testing.T) {
	dir := runFixtureProject(t)
	creator := newMockCreator()
	mb := &mockBackend{output: twoFindingsJSON}
	cfg := newRunConfig(dir, mb, creator, nil)

	res, err := Run(context.Background(), cfg, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Findings) != 2 {
		t.Fatalf("dry-run must still return findings, got %d", len(res.Findings))
	}
	if creator.count() != 0 {
		t.Fatalf("dry-run must create nothing, got %d", creator.count())
	}
	if res.Created != 2 {
		t.Errorf("dry-run would-create = %d, want 2", res.Created)
	}
}

func TestRun_DedupViaSearchCarriedThrough(t *testing.T) {
	dir := runFixtureProject(t)
	creator := newMockCreator()
	searcher := newMockSearcher()
	// Mark the first finding as already filed.
	already := finding("Split oversized.go", "refactor", pilotapi.RiskHigh, "oversized.go")
	searcher.existing[MarkerFor(ProposalHash(already))] = 1
	mb := &mockBackend{output: twoFindingsJSON}
	cfg := newRunConfig(dir, mb, creator, searcher)

	res, err := Run(context.Background(), cfg, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Created != 1 || res.Skipped != 1 {
		t.Fatalf("created=%d skipped=%d, want 1/1 (one already filed)", res.Created, res.Skipped)
	}
	if creator.count() != 1 {
		t.Fatalf("only the un-filed finding should be created, got %d", creator.count())
	}
}

func TestRun_EmptyScanNoBackendCall(t *testing.T) {
	dir := t.TempDir() // no oversized files → no signals
	creator := newMockCreator()
	mb := &mockBackend{output: twoFindingsJSON}
	cfg := newRunConfig(dir, mb, creator, nil)

	res, err := Run(context.Background(), cfg, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mb.calls != 0 {
		t.Errorf("empty scan must not invoke the PROPOSE backend, got %d calls", mb.calls)
	}
	if len(res.Findings) != 0 || res.Created != 0 {
		t.Errorf("empty scan → no findings/issues, got %d findings %d created", len(res.Findings), res.Created)
	}
}

func TestRun_ProposeErrorPropagates(t *testing.T) {
	dir := runFixtureProject(t)
	mb := &mockBackend{err: errFixture}
	cfg := newRunConfig(dir, mb, newMockCreator(), nil)

	_, err := Run(context.Background(), cfg, RunOptions{})
	if err == nil {
		t.Fatal("a PROPOSE backend error must propagate from Run")
	}
	if !strings.Contains(err.Error(), "propose") {
		t.Errorf("error should be scoped to the propose stage, got %v", err)
	}
}

func TestRun_RequiresScannerAndAnalyzer(t *testing.T) {
	_, err := Run(context.Background(), RunConfig{}, RunOptions{})
	if err == nil {
		t.Fatal("Run with no Scanner/Analyzer must error")
	}
}

func TestRun_LimitRespected(t *testing.T) {
	dir := runFixtureProject(t)
	creator := newMockCreator()
	mb := &mockBackend{output: twoFindingsJSON}
	cfg := newRunConfig(dir, mb, creator, nil)

	res, err := Run(context.Background(), cfg, RunOptions{Limit: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("limit=1 must create 1, got %d", res.Created)
	}
}

func TestRunOptions_EffectiveLimit(t *testing.T) {
	if got := (RunOptions{Limit: 0}).effectiveLimit(); got != defaultEmitLimit {
		t.Errorf("zero limit = %d, want default %d", got, defaultEmitLimit)
	}
	if got := (RunOptions{Limit: -1}).effectiveLimit(); got != 0 {
		t.Errorf("negative limit must map to 0 (no cap), got %d", got)
	}
	if got := (RunOptions{Limit: 3}).effectiveLimit(); got != 3 {
		t.Errorf("explicit limit must pass through, got %d", got)
	}
}
