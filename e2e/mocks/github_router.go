package mocks

import (
	"net/http"
	"strings"
)

// handleRequest routes requests to appropriate handlers.
func (m *GitHubMock) handleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route based on path pattern
	switch {
	// GET /repos/{owner}/{repo}/issues
	case r.Method == "GET" && strings.HasSuffix(path, "/issues") && !strings.Contains(path, "/issues/"):
		m.handleListIssues(w, r)

	// GET /repos/{owner}/{repo}/issues/{number}
	case r.Method == "GET" && strings.Contains(path, "/issues/") && !strings.Contains(path, "/labels"):
		m.handleGetIssue(w, r)

	// POST /repos/{owner}/{repo}/issues
	case r.Method == "POST" && strings.HasSuffix(path, "/issues"):
		m.handleCreateIssue(w, r)

	// PATCH /repos/{owner}/{repo}/issues/{number}
	case r.Method == "PATCH" && strings.Contains(path, "/issues/"):
		m.handleUpdateIssue(w, r)

	// POST /repos/{owner}/{repo}/issues/{number}/labels
	case r.Method == "POST" && strings.Contains(path, "/labels"):
		m.handleAddLabel(w, r)

	// DELETE /repos/{owner}/{repo}/issues/{number}/labels/{label}
	case r.Method == "DELETE" && strings.Contains(path, "/labels/"):
		m.handleRemoveLabel(w, r)

	// POST /repos/{owner}/{repo}/issues/{number}/comments
	case r.Method == "POST" && strings.Contains(path, "/comments"):
		m.handleCreateComment(w, r)

	// GET /repos/{owner}/{repo}/pulls
	case r.Method == "GET" && strings.HasSuffix(path, "/pulls") && !strings.Contains(path, "/pulls/"):
		m.handleListPRs(w, r)

	// GET /repos/{owner}/{repo}/pulls/{number}
	case r.Method == "GET" && strings.Contains(path, "/pulls/") && !strings.Contains(path, "/merge"):
		m.handleGetPR(w, r)

	// POST /repos/{owner}/{repo}/pulls
	case r.Method == "POST" && strings.HasSuffix(path, "/pulls"):
		m.handleCreatePR(w, r)

	// PATCH /repos/{owner}/{repo}/pulls/{number}
	case r.Method == "PATCH" && strings.Contains(path, "/pulls/"):
		m.handleUpdatePR(w, r)

	// PUT /repos/{owner}/{repo}/pulls/{number}/merge
	case r.Method == "PUT" && strings.Contains(path, "/merge"):
		m.handleMergePR(w, r)

	// GET /repos/{owner}/{repo}/commits/{sha}/check-runs
	case r.Method == "GET" && strings.Contains(path, "/check-runs"):
		m.handleGetCheckRuns(w, r)

	// GET /repos/{owner}/{repo}/branches/{branch}
	case r.Method == "GET" && strings.Contains(path, "/branches/"):
		m.handleGetBranch(w, r)

	default:
		w.WriteHeader(http.StatusOK)
	}
}
