package autopilot

import (
	"context"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// --- shared guardrails test doubles -----------------------------------------

// mockGuardrailsGH records every call the gate makes so tests can assert that
// the gate posts exactly the status/comment it should — and nothing more.
//
// The PR-body and comment fields drive the exception + dedup paths: prBody and
// comments are read back by scanPR. Zero values (empty body, no comments) mean
// "no exceptions, no prior comment", which is what every legacy test expects.
type mockGuardrailsGH struct {
	files    []*github.PRFile
	filesErr error

	prBody     string
	prErr      error
	comments   []*github.Comment
	listCmtErr error

	statusErr  error
	commentErr error
	updateErr  error

	statusCalls  []*github.CommitStatus
	statusSHAs   []string
	commentBody  []string
	commentPRs   []int
	updatedIDs   []int64
	updatedBody  []string
	listPRNumber int
	listCalls    int
}

func (m *mockGuardrailsGH) ListPullRequestFiles(_ context.Context, _, _ string, number int) ([]*github.PRFile, error) {
	m.listCalls++
	m.listPRNumber = number
	if m.filesErr != nil {
		return nil, m.filesErr
	}
	return m.files, nil
}

func (m *mockGuardrailsGH) CreateCommitStatus(_ context.Context, _, _, sha string, status *github.CommitStatus) (*github.CommitStatus, error) {
	m.statusCalls = append(m.statusCalls, status)
	m.statusSHAs = append(m.statusSHAs, sha)
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return status, nil
}

func (m *mockGuardrailsGH) GetPullRequest(_ context.Context, _, _ string, _ int) (*github.PullRequest, error) {
	if m.prErr != nil {
		return nil, m.prErr
	}
	return &github.PullRequest{Body: m.prBody}, nil
}

func (m *mockGuardrailsGH) ListIssueComments(_ context.Context, _, _ string, _ int) ([]*github.Comment, error) {
	if m.listCmtErr != nil {
		return nil, m.listCmtErr
	}
	return m.comments, nil
}

func (m *mockGuardrailsGH) AddPRComment(_ context.Context, _, _ string, number int, body string) (*github.PRComment, error) {
	m.commentBody = append(m.commentBody, body)
	m.commentPRs = append(m.commentPRs, number)
	if m.commentErr != nil {
		return nil, m.commentErr
	}
	return &github.PRComment{Body: body}, nil
}

func (m *mockGuardrailsGH) UpdateIssueComment(_ context.Context, _, _ string, commentID int64, body string) (*github.Comment, error) {
	m.updatedIDs = append(m.updatedIDs, commentID)
	m.updatedBody = append(m.updatedBody, body)
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return &github.Comment{ID: commentID, Body: body}, nil
}

// stubRegistry plants a fixed set of violations regardless of input, and records
// the disabled-rule list it was handed so a test can assert the passthrough.
type stubRegistry struct {
	out          []architect.Violation
	gotDisabled  []string
	gotChanged   []string
	gotWorktree  string
	evaluateCall int
}

func (s *stubRegistry) Evaluate(_ context.Context, changed []string, worktree string, disabled []string) []architect.Violation {
	s.evaluateCall++
	s.gotChanged = changed
	s.gotWorktree = worktree
	s.gotDisabled = disabled
	return s.out
}

func prFiles(names ...string) []*github.PRFile {
	out := make([]*github.PRFile, 0, len(names))
	for _, n := range names {
		out = append(out, &github.PRFile{Filename: n, Status: "modified"})
	}
	return out
}

func sampleViolations() []architect.Violation {
	return []architect.Violation{
		{Rule: "loc-400", File: "internal/x/big.go", Detail: "file is 450 lines (limit 400)", Risk: pilotapi.RiskMedium},
	}
}
