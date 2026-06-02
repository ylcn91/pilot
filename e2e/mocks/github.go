// Package mocks provides mock implementations for E2E testing.
package mocks

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// GitHubMock provides a mock GitHub API server for E2E testing.
// It tracks state across requests to simulate real GitHub behavior.
type GitHubMock struct {
	server      *httptest.Server
	mu          sync.RWMutex
	issues      map[int]*github.Issue
	prs         map[int]*github.PullRequest
	checkRuns   map[string]*github.CheckRunsResponse // keyed by SHA
	nextIssue   int
	nextPR      int
	nextComment int

	// Callbacks for test assertions
	OnIssueLabelAdded   func(issueNum int, label string)
	OnIssueLabelRemoved func(issueNum int, label string)
	OnPRCreated         func(pr *github.PullRequest)
	OnPRMerged          func(prNum int)
	OnCommentCreated    func(issueNum int, body string)
}

// NewGitHubMock creates a new mock GitHub API server.
func NewGitHubMock() *GitHubMock {
	m := &GitHubMock{
		issues:      make(map[int]*github.Issue),
		prs:         make(map[int]*github.PullRequest),
		checkRuns:   make(map[string]*github.CheckRunsResponse),
		nextIssue:   1,
		nextPR:      1,
		nextComment: 1,
	}

	m.server = httptest.NewServer(http.HandlerFunc(m.handleRequest))
	return m
}

// URL returns the base URL of the mock server.
func (m *GitHubMock) URL() string {
	return m.server.URL
}

// Close shuts down the mock server.
func (m *GitHubMock) Close() {
	m.server.Close()
}

// CreateIssue adds an issue to the mock.
func (m *GitHubMock) CreateIssue(title, body string, labels []string) *github.Issue {
	m.mu.Lock()
	defer m.mu.Unlock()

	issueLabels := make([]github.Label, len(labels))
	for i, l := range labels {
		issueLabels[i] = github.Label{Name: l}
	}

	issue := &github.Issue{
		Number:    m.nextIssue,
		Title:     title,
		Body:      body,
		State:     "open",
		Labels:    issueLabels,
		CreatedAt: time.Now(),
	}
	m.issues[m.nextIssue] = issue
	m.nextIssue++
	return issue
}

// GetIssue retrieves an issue by number.
func (m *GitHubMock) GetIssue(num int) *github.Issue {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.issues[num]
}

// CreatePR creates a PR in the mock (for pre-populating state before tests).
func (m *GitHubMock) CreatePR(number int, title, branchName, sha string) *github.PullRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	mergeable := true
	pr := &github.PullRequest{
		Number:         number,
		Title:          title,
		State:          "open",
		Merged:         false,
		Mergeable:      &mergeable,
		MergeableState: "clean",
		Head: github.PRRef{
			Ref: branchName,
			SHA: sha,
		},
		Base: github.PRRef{
			Ref: "main",
		},
		HTMLURL: m.server.URL + "/owner/repo/pull/" + strconv.Itoa(number),
	}
	m.prs[number] = pr

	// Update nextPR if needed
	if number >= m.nextPR {
		m.nextPR = number + 1
	}

	return pr
}

// SetCIStatus sets the CI check status for a given SHA.
func (m *GitHubMock) SetCIStatus(sha string, checks []github.CheckRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkRuns[sha] = &github.CheckRunsResponse{
		TotalCount: len(checks),
		CheckRuns:  checks,
	}
}

// SetCIPassing sets all required checks as passing for a SHA.
func (m *GitHubMock) SetCIPassing(sha string, checkNames []string) {
	checks := make([]github.CheckRun, len(checkNames))
	for i, name := range checkNames {
		checks[i] = github.CheckRun{
			Name:       name,
			Status:     "completed",
			Conclusion: "success",
		}
	}
	m.SetCIStatus(sha, checks)
}

// SetCIFailing sets a check as failing for a SHA.
func (m *GitHubMock) SetCIFailing(sha string, failingCheck string, passingChecks []string) {
	checks := make([]github.CheckRun, 0, len(passingChecks)+1)
	checks = append(checks, github.CheckRun{
		Name:       failingCheck,
		Status:     "completed",
		Conclusion: "failure",
	})
	for _, name := range passingChecks {
		checks = append(checks, github.CheckRun{
			Name:       name,
			Status:     "completed",
			Conclusion: "success",
		})
	}
	m.SetCIStatus(sha, checks)
}

// GetPR retrieves a PR by number.
func (m *GitHubMock) GetPR(num int) *github.PullRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prs[num]
}

// GetOpenPRs returns all open PRs.
func (m *GitHubMock) GetOpenPRs() []*github.PullRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var prs []*github.PullRequest
	for _, pr := range m.prs {
		if pr.State == "open" {
			prs = append(prs, pr)
		}
	}
	return prs
}

// Helper methods to extract IDs from paths

func (m *GitHubMock) extractIssueNumber(path string) int {
	// /repos/owner/repo/issues/123 or /repos/owner/repo/issues/123/labels
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "issues" && i+1 < len(parts) {
			numStr := strings.Split(parts[i+1], "/")[0]
			num, _ := strconv.Atoi(numStr)
			return num
		}
	}
	return 0
}

func (m *GitHubMock) extractPRNumber(path string) int {
	// /repos/owner/repo/pulls/123 or /repos/owner/repo/pulls/123/merge
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "pulls" && i+1 < len(parts) {
			numStr := strings.Split(parts[i+1], "/")[0]
			num, _ := strconv.Atoi(numStr)
			return num
		}
	}
	return 0
}

func (m *GitHubMock) extractSHA(path string) string {
	// /repos/owner/repo/commits/abc123/check-runs
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "commits" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (m *GitHubMock) extractLabel(path string) string {
	// /repos/owner/repo/issues/123/labels/pilot-in-progress
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "labels" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
