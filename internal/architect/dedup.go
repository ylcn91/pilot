package architect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// markerPrefix is the stable token that identifies a hidden Architect dedup
// marker inside an issue body. The full marker is an HTML comment so it renders
// invisibly on GitHub while remaining searchable as literal text.
const markerPrefix = "pilot-architect:"

// ProposalHash returns a deterministic identity for a proposal, used both to
// dedup within a single run and to recognise a proposal that was already filed
// as an issue. The identity is derived only from the proposal's Kind and the
// set of Files it touches — deliberately NOT from its prose (Title,
// WhyItMatters, …), so two runs that phrase the same structural change
// differently still collapse to one issue.
//
// Files are sorted before hashing, making the hash order-insensitive: a
// proposal listing [a.go, b.go] and one listing [b.go, a.go] share a hash.
// Hashing is delegated to pilotapi.TraceHash so the Architect family uses the
// same trace primitive as the rest of the handoff protocol.
func ProposalHash(f pilotapi.Finding) string {
	files := make([]string, 0, len(f.Files))
	for _, p := range f.Files {
		if t := strings.TrimSpace(p); t != "" {
			files = append(files, t)
		}
	}
	sort.Strings(files)
	return pilotapi.TraceHash(strings.TrimSpace(f.Kind), strings.Join(files, "\n"))
}

// MarkerFor returns the hidden HTML-comment marker that embeds hash into an
// issue body. The marker round-trips through GitHub's Search API as literal
// text, so SearchExisting can find a previously-filed proposal by it while it
// stays invisible in the rendered issue.
func MarkerFor(hash string) string {
	return fmt.Sprintf("<!-- %s%s -->", markerPrefix, hash)
}

// IssueSearcher is the minimal Search-API surface the Deduper needs to detect
// an already-filed proposal. It is defined here (rather than importing the
// github client directly) so the EMIT stage can be unit-tested with a stub that
// makes no network calls. *github.Client satisfies it via SearchIssuesContaining.
type IssueSearcher interface {
	// SearchIssuesContaining reports how many issues in owner/repo contain the
	// given literal phrase (the dedup marker). A count > 0 means the proposal
	// was already filed.
	SearchIssuesContaining(ctx context.Context, owner, repo, phrase string) (int, error)
}

// Deduper tracks which proposal hashes have already been handled so a single
// Emit pass never files the same structural change twice, and consults an
// IssueSearcher to recognise proposals already filed in a previous run.
//
// The in-memory seen-set is per-Deduper (per run); SearchExisting reaches out
// to GitHub for cross-run dedup. A nil searcher disables the remote check
// (SearchExisting always reports "not found"), which is the correct behaviour
// for a dry run that performs no I/O.
type Deduper struct {
	searcher IssueSearcher
	seen     map[string]bool
}

// NewDeduper builds a Deduper backed by searcher (may be nil to disable the
// remote existence check). The seen-set starts empty.
func NewDeduper(searcher IssueSearcher) *Deduper {
	return &Deduper{
		searcher: searcher,
		seen:     make(map[string]bool),
	}
}

// Seen reports whether hash has already been marked in this run.
func (d *Deduper) Seen(hash string) bool {
	return d.seen[hash]
}

// MarkSeen records hash as handled so a later proposal with the same hash in the
// same run is recognised as an intra-run duplicate.
func (d *Deduper) MarkSeen(hash string) {
	d.seen[hash] = true
}

// SearchExisting reports whether an issue carrying marker already exists in
// owner/repo. With no searcher configured it always returns (false, nil): the
// remote check is simply skipped. A searcher error is propagated so the caller
// can decide whether to fail the run or proceed; it is never silently treated
// as "exists" (which would suppress a real proposal) nor as "not exists" by the
// Deduper itself.
func (d *Deduper) SearchExisting(ctx context.Context, owner, repo, marker string) (bool, error) {
	if d.searcher == nil {
		return false, nil
	}
	count, err := d.searcher.SearchIssuesContaining(ctx, owner, repo, marker)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
