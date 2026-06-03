package architect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestProposalHash_Deterministic(t *testing.T) {
	f := finding("Split big.go", "refactor", pilotapi.RiskHigh, "a.go", "b.go")
	h1 := ProposalHash(f)
	h2 := ProposalHash(f)
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
	if h1 == "" {
		t.Fatal("hash must be non-empty")
	}
	if len(h1) != 12 {
		t.Errorf("hash len = %d, want 12 (pilotapi.TraceHash truncation)", len(h1))
	}
}

func TestProposalHash_FileOrderInsensitive(t *testing.T) {
	a := finding("t", "refactor", pilotapi.RiskHigh, "a.go", "b.go", "c.go")
	b := finding("t", "refactor", pilotapi.RiskHigh, "c.go", "a.go", "b.go")
	if ProposalHash(a) != ProposalHash(b) {
		t.Fatalf("hash must be order-insensitive over files: %q vs %q", ProposalHash(a), ProposalHash(b))
	}
}

func TestProposalHash_IgnoresProse(t *testing.T) {
	a := finding("Split big.go", "refactor", pilotapi.RiskHigh, "a.go")
	b := a
	b.Title = "Totally different wording"
	b.WhyItMatters = "different rationale"
	b.TestPlan = "different plan"
	b.Risk = pilotapi.RiskLow
	if ProposalHash(a) != ProposalHash(b) {
		t.Fatal("hash must depend only on Kind+Files, not on prose or risk")
	}
}

func TestProposalHash_KindSensitive(t *testing.T) {
	a := finding("t", "refactor", pilotapi.RiskHigh, "a.go")
	b := finding("t", "bug", pilotapi.RiskHigh, "a.go")
	if ProposalHash(a) == ProposalHash(b) {
		t.Fatal("different Kind must produce a different hash")
	}
}

func TestProposalHash_FilesSensitive(t *testing.T) {
	a := finding("t", "refactor", pilotapi.RiskHigh, "a.go")
	b := finding("t", "refactor", pilotapi.RiskHigh, "a.go", "b.go")
	if ProposalHash(a) == ProposalHash(b) {
		t.Fatal("different Files must produce a different hash")
	}
}

func TestProposalHash_TrimsAndDropsBlankFiles(t *testing.T) {
	a := finding("t", "refactor", pilotapi.RiskHigh, "a.go", "b.go")
	b := finding("t", " refactor ", pilotapi.RiskHigh, " a.go ", "", "  ", "b.go")
	if ProposalHash(a) != ProposalHash(b) {
		t.Fatalf("whitespace/blank entries must not change the hash: %q vs %q", ProposalHash(a), ProposalHash(b))
	}
}

func TestProposalHash_NoFiles(t *testing.T) {
	a := finding("t", "refactor", pilotapi.RiskHigh)
	if ProposalHash(a) == "" {
		t.Fatal("a fileless proposal must still hash to a stable non-empty value")
	}
	if ProposalHash(a) != ProposalHash(finding("other title", "refactor", pilotapi.RiskLow)) {
		t.Fatal("two fileless same-kind proposals must collapse")
	}
}

func TestMarkerFor_Format(t *testing.T) {
	m := MarkerFor("abc123")
	if m != "<!-- pilot-architect:abc123 -->" {
		t.Fatalf("marker = %q, want HTML comment with pilot-architect: prefix", m)
	}
	if !strings.HasPrefix(m, "<!--") || !strings.HasSuffix(m, "-->") {
		t.Error("marker must be a valid HTML comment so it renders invisibly")
	}
	if !strings.Contains(m, markerPrefix) {
		t.Error("marker must contain the searchable prefix")
	}
}

func TestDeduper_SeenSet(t *testing.T) {
	d := NewDeduper(nil)
	if d.Seen("h") {
		t.Fatal("fresh Deduper must not report any hash as seen")
	}
	d.MarkSeen("h")
	if !d.Seen("h") {
		t.Fatal("MarkSeen must make the hash report as seen")
	}
	if d.Seen("other") {
		t.Fatal("unrelated hash must not be seen")
	}
}

func TestDeduper_SearchExisting_NilSearcher(t *testing.T) {
	d := NewDeduper(nil)
	exists, err := d.SearchExisting(context.Background(), "o", "r", MarkerFor("h"))
	if err != nil {
		t.Fatalf("nil searcher must not error: %v", err)
	}
	if exists {
		t.Fatal("nil searcher must report not-existing (remote check skipped)")
	}
}

func TestDeduper_SearchExisting_Found(t *testing.T) {
	s := newMockSearcher()
	marker := MarkerFor("h")
	s.existing[marker] = 1
	d := NewDeduper(s)

	exists, err := d.SearchExisting(context.Background(), "o", "r", marker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("a marker with a positive search count must report existing")
	}
	if len(s.queries) != 1 || s.queries[0] != marker {
		t.Fatalf("searcher should be queried once with the marker, got %v", s.queries)
	}
}

func TestDeduper_SearchExisting_NotFound(t *testing.T) {
	s := newMockSearcher()
	d := NewDeduper(s)
	exists, err := d.SearchExisting(context.Background(), "o", "r", MarkerFor("absent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("a marker with zero matches must report not-existing")
	}
}

func TestDeduper_SearchExisting_PropagatesError(t *testing.T) {
	s := newMockSearcher()
	s.err = errors.New("rate limited")
	d := NewDeduper(s)
	exists, err := d.SearchExisting(context.Background(), "o", "r", MarkerFor("h"))
	if err == nil {
		t.Fatal("searcher error must be propagated, not swallowed")
	}
	if exists {
		t.Fatal("on error, existing must be false so the caller decides")
	}
}
