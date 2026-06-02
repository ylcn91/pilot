package architect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestEmit_DryRunCreatesNothing(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "o", "r", nil)
	findings := []pilotapi.Finding{
		finding("Split a", "refactor", pilotapi.RiskHigh, "a.go"),
		finding("Split b", "refactor", pilotapi.RiskMedium, "b.go"),
	}

	created, skipped, err := e.Emit(context.Background(), findings, true, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if creator.count() != 0 {
		t.Fatalf("dry-run must create nothing, got %d creates", creator.count())
	}
	if created != 2 {
		t.Errorf("dry-run created (would-create) = %d, want 2", created)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

func TestEmit_DryRunWithNilCreator(t *testing.T) {
	// A dry run must work even with no creator wired (the unconfigured-GitHub path).
	e := NewEmitter(nil, nil, "o", "r", nil)
	created, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, true, 0)
	if err != nil {
		t.Fatalf("dry-run with nil creator must not error: %v", err)
	}
	if created != 1 {
		t.Errorf("would-create = %d, want 1", created)
	}
}

func TestEmit_CreatesIssues(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "owner", "repo", []string{"pilot", "architect"})
	findings := []pilotapi.Finding{
		finding("Split a", "refactor", pilotapi.RiskHigh, "a.go"),
		finding("Harden b", "hardening", pilotapi.RiskMedium, "b.go"),
	}

	created, skipped, err := e.Emit(context.Background(), findings, false, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if created != 2 || skipped != 0 {
		t.Fatalf("created=%d skipped=%d, want 2/0", created, skipped)
	}
	if creator.count() != 2 {
		t.Fatalf("expected 2 create calls, got %d", creator.count())
	}
	first := creator.calls[0]
	if first.owner != "owner" || first.repo != "repo" {
		t.Errorf("create target = %s/%s, want owner/repo", first.owner, first.repo)
	}
	if strings.Join(first.labels, ",") != "pilot,architect" {
		t.Errorf("labels = %v, want [pilot architect]", first.labels)
	}
}

func TestEmit_DedupBySearch(t *testing.T) {
	creator := newMockCreator()
	searcher := newMockSearcher()
	f := finding("Split a", "refactor", pilotapi.RiskHigh, "a.go")
	searcher.existing[MarkerFor(ProposalHash(f))] = 1 // already filed in a previous run
	e := NewEmitter(creator, searcher, "o", "r", nil)

	created, skipped, err := e.Emit(context.Background(), []pilotapi.Finding{f}, false, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if created != 0 || skipped != 1 {
		t.Fatalf("created=%d skipped=%d, want 0/1 (search dedup)", created, skipped)
	}
	if creator.count() != 0 {
		t.Fatalf("a search-deduped finding must not be created, got %d", creator.count())
	}
}

func TestEmit_IntraRunDedup(t *testing.T) {
	creator := newMockCreator()
	searcher := newMockSearcher()
	// Two findings that hash identically (same kind+files, different prose).
	a := finding("Split a one way", "refactor", pilotapi.RiskHigh, "a.go")
	b := finding("Split a other way", "refactor", pilotapi.RiskHigh, "a.go")
	if ProposalHash(a) != ProposalHash(b) {
		t.Fatal("test precondition: the two findings must share a hash")
	}
	e := NewEmitter(creator, searcher, "o", "r", nil)

	created, skipped, err := e.Emit(context.Background(), []pilotapi.Finding{a, b}, false, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if created != 1 || skipped != 1 {
		t.Fatalf("created=%d skipped=%d, want 1/1 (intra-run dedup)", created, skipped)
	}
	if creator.count() != 1 {
		t.Fatalf("same-hash findings must yield exactly one create, got %d", creator.count())
	}
	// The second (already-seen) finding must short-circuit BEFORE searching.
	if len(searcher.queries) != 1 {
		t.Errorf("searcher should be hit once (for the first finding), got %d queries", len(searcher.queries))
	}
}

func TestEmit_TopNLimit(t *testing.T) {
	creator := newMockCreator()
	findings := []pilotapi.Finding{
		finding("a", "refactor", pilotapi.RiskHigh, "a.go"),
		finding("b", "refactor", pilotapi.RiskHigh, "b.go"),
		finding("c", "refactor", pilotapi.RiskHigh, "c.go"),
		finding("d", "refactor", pilotapi.RiskHigh, "d.go"),
	}
	e := NewEmitter(creator, nil, "o", "r", nil)

	created, _, err := e.Emit(context.Background(), findings, false, 2)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if created != 2 {
		t.Fatalf("limit=2 must create 2, got %d", created)
	}
	if creator.count() != 2 {
		t.Fatalf("expected 2 creates under limit, got %d", creator.count())
	}
}

func TestEmit_RiskOrdering(t *testing.T) {
	creator := newMockCreator()
	// Deliberately scrambled risk order on input.
	findings := []pilotapi.Finding{
		finding("low one", "refactor", pilotapi.RiskLow, "low.go"),
		finding("blocker one", "bug", pilotapi.RiskReleaseBlocker, "blocker.go"),
		finding("medium one", "refactor", pilotapi.RiskMedium, "med.go"),
		finding("high one", "refactor", pilotapi.RiskHigh, "high.go"),
	}
	e := NewEmitter(creator, nil, "o", "r", nil)

	// Limit to top 2 → must be the release-blocker then the high.
	created, _, err := e.Emit(context.Background(), findings, false, 2)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if created != 2 {
		t.Fatalf("created = %d, want 2", created)
	}
	if !strings.Contains(creator.calls[0].body, "blocker.go") {
		t.Errorf("first emitted issue must be the release-blocker, got body:\n%s", creator.calls[0].body)
	}
	if !strings.Contains(creator.calls[1].body, "high.go") {
		t.Errorf("second emitted issue must be the high-risk one, got body:\n%s", creator.calls[1].body)
	}
}

func TestEmit_TitleIsConventionalCommit(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "o", "r", nil)
	_, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("split the oversized scanner file", "refactor", pilotapi.RiskHigh, "internal/architect/scanner.go"),
	}, false, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	title := creator.calls[0].title
	if !titleHasConventionalPrefix.MatchString(title) {
		t.Fatalf("title %q is not conventional-commit format", title)
	}
	if !strings.HasPrefix(title, "refactor(") {
		t.Errorf("non-conventional input title should be wrapped as refactor(scope): …, got %q", title)
	}
}

func TestEmit_SearchErrorAborts(t *testing.T) {
	creator := newMockCreator()
	searcher := newMockSearcher()
	searcher.err = errors.New("api down")
	e := NewEmitter(creator, searcher, "o", "r", nil)

	_, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, false, 0)
	if err == nil {
		t.Fatal("a dedup search error must abort the emit")
	}
	if creator.count() != 0 {
		t.Fatal("no issue should be created when the dedup search fails")
	}
}

func TestEmit_CreateErrorAborts(t *testing.T) {
	creator := newMockCreator()
	creator.err = errors.New("403")
	e := NewEmitter(creator, nil, "o", "r", nil)

	created, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("a", "refactor", pilotapi.RiskHigh, "a.go"),
		finding("b", "refactor", pilotapi.RiskHigh, "b.go"),
	}, false, 0)
	if err == nil {
		t.Fatal("a create error must abort the emit")
	}
	if created != 0 {
		t.Errorf("created = %d, want 0 on create failure", created)
	}
}

func TestEmit_NonDryRunNilCreatorErrors(t *testing.T) {
	e := NewEmitter(nil, nil, "o", "r", nil)
	_, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, false, 0)
	if err == nil {
		t.Fatal("non-dry-run emit with no creator must error rather than silently no-op")
	}
}

func TestEmit_EmptyFindings(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "o", "r", nil)
	created, skipped, err := e.Emit(context.Background(), nil, false, 0)
	if err != nil {
		t.Fatalf("empty findings must not error: %v", err)
	}
	if created != 0 || skipped != 0 {
		t.Fatalf("empty findings → 0/0, got %d/%d", created, skipped)
	}
}

func TestEmit_DefaultLabelsApplied(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "o", "r", nil) // nil labels → defaults
	_, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, false, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := strings.Join(creator.calls[0].labels, ",")
	if got != "pilot,architect" {
		t.Errorf("default labels = %q, want pilot,architect", got)
	}
}

func TestEmit_ContextCancelled(t *testing.T) {
	creator := newMockCreator()
	e := NewEmitter(creator, nil, "o", "r", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := e.Emit(ctx, []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, false, 0)
	if err == nil {
		t.Fatal("a cancelled context must abort emit")
	}
	if creator.count() != 0 {
		t.Fatal("nothing should be created under a cancelled context")
	}
}
