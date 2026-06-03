package architect

import (
	"context"
	"sync"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// mockSearcher is an IssueSearcher stub. existing maps a marker phrase to the
// count SearchIssuesContaining should report; absent markers report 0. err, when
// set, is returned instead. queries records every phrase searched, in order.
type mockSearcher struct {
	mu       sync.Mutex
	existing map[string]int
	err      error
	queries  []string
}

func newMockSearcher() *mockSearcher {
	return &mockSearcher{existing: make(map[string]int)}
}

func (m *mockSearcher) SearchIssuesContaining(_ context.Context, _, _, phrase string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queries = append(m.queries, phrase)
	if m.err != nil {
		return 0, m.err
	}
	return m.existing[phrase], nil
}

// mockCreator is an IssueCreator stub. It records every create call, returns a
// monotonically-numbered issue, and can be made to fail.
type mockCreator struct {
	mu     sync.Mutex
	calls  []createCall
	err    error
	nextID int
}

type createCall struct {
	owner, repo, title, body string
	labels                   []string
}

func newMockCreator() *mockCreator { return &mockCreator{} }

func (m *mockCreator) CreatePilotIssue(_ context.Context, owner, repo, title, body string, labels []string) (*IssueRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	m.calls = append(m.calls, createCall{owner: owner, repo: repo, title: title, body: body, labels: labels})
	m.nextID++
	return &IssueRef{Number: m.nextID}, nil
}

func (m *mockCreator) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// finding is a terse constructor for a pilotapi.Finding used across emit/runner
// tests.
func finding(title, kind string, risk pilotapi.RiskLevel, files ...string) pilotapi.Finding {
	return pilotapi.Finding{
		Title:        title,
		Kind:         kind,
		Risk:         risk,
		WhyItMatters: "because " + title,
		TestPlan:     "go test ./...",
		Files:        files,
	}
}
