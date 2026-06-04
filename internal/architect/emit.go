package architect

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// defaultEmitLabels are applied to every issue the Architect files so they are
// pickable by the Pilot daemon (the "pilot" label) and visibly attributed to the
// proactive analysis path (the "architect" label).
var defaultEmitLabels = []string{"pilot", "architect"}

// IssueRef is the architect-local result of a successful issue creation. It
// carries only the field the EMIT stage actually consumes (the issue number,
// for logging), so the github.Issue type does not leak across the IssueCreator
// boundary. Backends that have no numeric identifier (e.g. Linear) leave Number
// zero.
type IssueRef struct {
	Number int
}

// IssueCreator is the minimal creation surface the Emitter needs. Defining it
// here (rather than calling github.CreatePilotIssue directly) lets the EMIT
// stage be unit-tested with a stub that records calls and makes no network
// request. The production implementation (clientIssueCreator) wraps a
// *github.Client and the package-level github.CreatePilotIssue chokepoint.
type IssueCreator interface {
	// CreatePilotIssue files an issue with the given conventional-commit title,
	// markdown body, and labels, returning a reference to the created issue. It
	// must enforce the same guardrails as github.CreatePilotIssue (issue-creation
	// enabled, repo allowlist, conventional-commit title).
	CreatePilotIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*IssueRef, error)
}

// clientIssueCreator adapts a *github.Client to IssueCreator. It enables issue
// creation on the client (required before github.CreatePilotIssue will act) and
// passes AllowAllIssueRepos because the owner/repo are already constrained by the
// caller's own configuration.
type clientIssueCreator struct {
	client *github.Client
}

// NewClientIssueCreator wraps client as an IssueCreator, enabling issue creation
// on it. Returns nil when client is nil so callers can detect an unconfigured
// GitHub adapter and fall back to dry-run.
func NewClientIssueCreator(client *github.Client) IssueCreator {
	if client == nil {
		return nil
	}
	client.SetIssueCreationEnabled(true)
	return &clientIssueCreator{client: client}
}

func (c *clientIssueCreator) CreatePilotIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*IssueRef, error) {
	issue, err := github.CreatePilotIssue(ctx, c.client, github.AllowAllIssueRepos(), owner, repo, title, body, labels)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, nil
	}
	return &IssueRef{Number: issue.Number}, nil
}

// SubIssueCreator is the minimal sub-issue creation surface the Linear EMIT
// backend needs. It mirrors executor.SubIssueCreator (which *linear.Client
// satisfies) but is redeclared here so the architect package depends only on a
// narrow seam, keeping the Linear emit path unit-testable with a network-free
// stub. CreateIssue files a child of parentID and returns the new issue's
// identifier and URL.
type SubIssueCreator interface {
	CreateIssue(ctx context.Context, parentID, title, body string, labels []string) (identifier string, url string, err error)
}

// linearIssueCreator adapts a SubIssueCreator (e.g. *linear.Client) to the
// Emitter's IssueCreator interface. Every Architect finding becomes one Linear
// sub-issue hung off parentID; the dedup marker travels inside body exactly as
// it does for GitHub, so MarkerFor still embeds and cross-run dedup still works.
// owner/repo from the Emitter are ignored — Linear derives team/project from the
// parent. The returned *IssueRef carries no number (unavailable from Linear's
// string identifier) so the Emitter's logging path stays uniform.
type linearIssueCreator struct {
	client   SubIssueCreator
	parentID string
}

// NewLinearIssueCreator wraps a SubIssueCreator as an IssueCreator that files
// every finding as a sub-issue under parentID. Returns nil when client is nil so
// callers can detect an unconfigured Linear adapter. parentID must be a real,
// existing Linear issue ID; the caller is responsible for rejecting an empty one
// before reaching a non-dry-run emit.
func NewLinearIssueCreator(client SubIssueCreator, parentID string) IssueCreator {
	if client == nil {
		return nil
	}
	return &linearIssueCreator{client: client, parentID: parentID}
}

func (c *linearIssueCreator) CreatePilotIssue(ctx context.Context, _, _, title, body string, labels []string) (*IssueRef, error) {
	if _, _, err := c.client.CreateIssue(ctx, c.parentID, title, body, labels); err != nil {
		return nil, err
	}
	return &IssueRef{}, nil
}

// Emitter is the EMIT stage: it turns ranked proposals into idempotent GitHub
// issues. It owns the issue creator, the dedup tracker, the target owner/repo,
// and the labels to apply. Emit is the single entry point.
type Emitter struct {
	creator           IssueCreator
	deduper           *Deduper
	owner             string
	repo              string
	labels            []string
	log               *slog.Logger
	quietDryRunOutput bool
}

// NewEmitter builds an Emitter targeting owner/repo. creator may be nil to force
// dry-run-only behaviour (Emit will refuse to create and return an error if a
// non-dry-run create is attempted). searcher (used to build the Deduper) may be
// nil to disable cross-run existence checks. When labels is empty the default
// pilot+architect labels are applied.
func NewEmitter(creator IssueCreator, searcher IssueSearcher, owner, repo string, labels []string) *Emitter {
	if len(labels) == 0 {
		labels = append([]string(nil), defaultEmitLabels...)
	}
	return &Emitter{
		creator: creator,
		deduper: NewDeduper(searcher),
		owner:   owner,
		repo:    repo,
		labels:  labels,
		log:     logging.WithComponent("architect.emit"),
	}
}

// SuppressDryRunOutput keeps dry-run issue previews out of stdout. The CLI uses
// this for --json so machine-readable output stays parseable.
func (e *Emitter) SuppressDryRunOutput() *Emitter {
	e.quietDryRunOutput = true
	return e
}

// Emit files issues for the top-N proposals, deduping along the way, and reports
// how many were created and skipped.
//
// Ranking: proposals are ordered by Risk severity (release-blocker first), with
// ties broken by the order they arrive in — the Analyzer already returns them
// weight-ranked, so this preserves "risk, then weight". Only the first limit
// proposals (limit <= 0 means no cap) are considered for emission.
//
// Dedup: for each candidate the structural ProposalHash is computed. A hash
// already seen in this run, or one whose marker an existing issue carries
// (SearchExisting), is skipped. Otherwise the hash is recorded and the issue is
// built and created.
//
// dryRun: when true, nothing is created. Each would-be issue's title and body are
// logged, the hash is still recorded (so intra-run dups are still collapsed in
// the dry-run report), and skipped reflects only true dedups — never the dry-run
// itself. The returned created count is the number that WOULD have been created.
func (e *Emitter) Emit(ctx context.Context, findings []pilotapi.Finding, dryRun bool, limit int) (created int, skipped int, err error) {
	ranked := rankFindings(findings)
	return e.emit(ctx, ranked, dryRun, limit)
}

// EmitOrdered files findings in caller-provided order. It keeps the same dedup,
// dry-run, and limit semantics as Emit, but deliberately bypasses risk ranking so
// precomputed handoff sequences (for example a blast-radius-ordered refactor
// plan) retain their lineage order in the target tracker.
func (e *Emitter) EmitOrdered(ctx context.Context, findings []pilotapi.Finding, dryRun bool, limit int) (created int, skipped int, err error) {
	ordered := make([]pilotapi.Finding, len(findings))
	copy(ordered, findings)
	return e.emit(ctx, ordered, dryRun, limit)
}

func (e *Emitter) emit(ctx context.Context, ranked []pilotapi.Finding, dryRun bool, limit int) (created int, skipped int, err error) {
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}

	for _, f := range ranked {
		if err := ctx.Err(); err != nil {
			return created, skipped, err
		}

		hash := ProposalHash(f)
		marker := MarkerFor(hash)

		if e.deduper.Seen(hash) {
			skipped++
			continue
		}

		exists, searchErr := e.deduper.SearchExisting(ctx, e.owner, e.repo, marker)
		if searchErr != nil {
			return created, skipped, fmt.Errorf("architect: dedup search for %q: %w", f.Title, searchErr)
		}
		if exists {
			e.deduper.MarkSeen(hash)
			skipped++
			continue
		}

		e.deduper.MarkSeen(hash)
		title := proposalTitle(f)
		body := proposalBody(f, marker)

		if dryRun {
			e.log.Info("dry-run: would create issue",
				slog.String("title", title),
				slog.String("hash", hash),
			)
			if !e.quietDryRunOutput {
				fmt.Printf("--- architect (dry-run) would create issue ---\n%s\n\n%s\n\n", title, body)
			}
			created++
			continue
		}

		if e.creator == nil {
			return created, skipped, fmt.Errorf("architect: no issue creator configured for non-dry-run emit")
		}

		issue, createErr := e.creator.CreatePilotIssue(ctx, e.owner, e.repo, title, body, e.labels)
		if createErr != nil {
			return created, skipped, fmt.Errorf("architect: create issue %q: %w", title, createErr)
		}
		e.log.Info("created proactive refactor issue",
			slog.Int("number", issueNumber(issue)),
			slog.String("title", title),
			slog.String("hash", hash),
		)
		created++
	}

	return created, skipped, nil
}

// issueNumber safely extracts the issue number from a possibly-nil ref (a stub
// creator may return nil on success).
func issueNumber(i *IssueRef) int {
	if i == nil {
		return 0
	}
	return i.Number
}

// rankFindings returns a copy of findings ordered by descending Risk severity,
// stable within a severity bucket so the Analyzer's weight ordering is preserved
// as the secondary key.
func rankFindings(findings []pilotapi.Finding) []pilotapi.Finding {
	out := make([]pilotapi.Finding, len(findings))
	copy(out, findings)
	sort.SliceStable(out, func(i, j int) bool {
		return riskRank(out[i].Risk) > riskRank(out[j].Risk)
	})
	return out
}

// riskRank maps a RiskLevel onto an integer severity for ordering. Unknown or
// empty risks rank below the canonical levels so they sort last.
func riskRank(r pilotapi.RiskLevel) int {
	switch r {
	case pilotapi.RiskReleaseBlocker:
		return 4
	case pilotapi.RiskHigh:
		return 3
	case pilotapi.RiskMedium:
		return 2
	case pilotapi.RiskLow:
		return 1
	default:
		return 0
	}
}
