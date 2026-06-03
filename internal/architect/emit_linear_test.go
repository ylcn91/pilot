package architect

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// mockLinearCreator is a SubIssueCreator stub that records every CreateIssue call
// and makes no network request. It returns a monotonically-numbered Linear
// identifier so the emit path can assert one call per finding.
type mockLinearCreator struct {
	mu     sync.Mutex
	calls  []linearCreateCall
	err    error
	nextID int
}

type linearCreateCall struct {
	parentID, title, body string
	labels                []string
}

func (m *mockLinearCreator) CreateIssue(_ context.Context, parentID, title, body string, labels []string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", "", m.err
	}
	m.calls = append(m.calls, linearCreateCall{parentID: parentID, title: title, body: body, labels: labels})
	m.nextID++
	id := "APP-" + string(rune('0'+m.nextID))
	return id, "https://linear.app/issue/" + id, nil
}

func (m *mockLinearCreator) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestLinearEmit_CreatesSubIssuesUnderParent(t *testing.T) {
	creator := &mockLinearCreator{}
	e := NewEmitter(NewLinearIssueCreator(creator, "PARENT-1"), nil, "", "", nil)

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
		t.Fatalf("expected 2 Linear create calls, got %d", creator.count())
	}
	for i, c := range creator.calls {
		if c.parentID != "PARENT-1" {
			t.Errorf("call %d parentID = %q, want PARENT-1", i, c.parentID)
		}
		// The dedup marker must travel in the body just like the GitHub path.
		if !strings.Contains(c.body, markerPrefix) {
			t.Errorf("call %d body missing dedup marker, got:\n%s", i, c.body)
		}
	}
}

func TestLinearEmit_DedupBySearch(t *testing.T) {
	creator := &mockLinearCreator{}
	searcher := newMockSearcher()
	f := finding("Split a", "refactor", pilotapi.RiskHigh, "a.go")
	searcher.existing[MarkerFor(ProposalHash(f))] = 1 // already filed in a previous Linear run

	e := NewEmitter(NewLinearIssueCreator(creator, "PARENT-1"), searcher, "", "", nil)
	created, skipped, err := e.Emit(context.Background(), []pilotapi.Finding{f}, false, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if created != 0 || skipped != 1 {
		t.Fatalf("created=%d skipped=%d, want 0/1 (Linear search dedup)", created, skipped)
	}
	if creator.count() != 0 {
		t.Fatalf("a search-deduped finding must not be created in Linear, got %d", creator.count())
	}
}

func TestLinearEmit_DryRunCreatesNothing(t *testing.T) {
	creator := &mockLinearCreator{}
	e := NewEmitter(NewLinearIssueCreator(creator, "PARENT-1"), nil, "", "", nil)
	created, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, true, 0)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if creator.count() != 0 {
		t.Fatalf("dry-run must create nothing in Linear, got %d", creator.count())
	}
	if created != 1 {
		t.Errorf("dry-run would-create = %d, want 1", created)
	}
}

func TestLinearEmit_CreateErrorAborts(t *testing.T) {
	creator := &mockLinearCreator{err: errors.New("linear 422")}
	e := NewEmitter(NewLinearIssueCreator(creator, "PARENT-1"), nil, "", "", nil)
	_, _, err := e.Emit(context.Background(), []pilotapi.Finding{
		finding("x", "refactor", pilotapi.RiskHigh, "a.go"),
	}, false, 0)
	if err == nil {
		t.Fatal("a Linear create error must abort the emit")
	}
}

func TestNewLinearIssueCreator_NilClient(t *testing.T) {
	if NewLinearIssueCreator(nil, "PARENT-1") != nil {
		t.Fatal("a nil client must yield a nil creator so callers can fall back to dry-run")
	}
}
