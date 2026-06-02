//go:build integration

package autopilot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// integrationMockNotifier captures notifications for verification
type integrationMockNotifier struct {
	mu            sync.Mutex
	mergedCalls   []*PRState
	ciFailedCalls []*PRState
	approvalCalls []*PRState
	fixIssueCalls []int
	releaseCalls  []string
}

func (m *integrationMockNotifier) NotifyMerged(ctx context.Context, prState *PRState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergedCalls = append(m.mergedCalls, prState)
	return nil
}

func (m *integrationMockNotifier) NotifyCIFailed(ctx context.Context, prState *PRState, failedChecks []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ciFailedCalls = append(m.ciFailedCalls, prState)
	return nil
}

func (m *integrationMockNotifier) NotifyApprovalRequired(ctx context.Context, prState *PRState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalCalls = append(m.approvalCalls, prState)
	return nil
}

func (m *integrationMockNotifier) NotifyFixIssueCreated(ctx context.Context, prState *PRState, issueNumber int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fixIssueCalls = append(m.fixIssueCalls, issueNumber)
	return nil
}

func (m *integrationMockNotifier) NotifyReleased(ctx context.Context, prState *PRState, releaseURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls = append(m.releaseCalls, releaseURL)
	return nil
}

// setupMockGitHubServer creates a test server that simulates GitHub API
func setupMockGitHubServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(handler))
}

func integrationBoolPtr(b bool) *bool {
	return &b
}
