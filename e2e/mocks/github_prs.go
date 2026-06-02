package mocks

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

func (m *GitHubMock) handleListPRs(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	prs := m.GetOpenPRs()
	m.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prs)
}

func (m *GitHubMock) handleGetPR(w http.ResponseWriter, r *http.Request) {
	num := m.extractPRNumber(r.URL.Path)
	if num == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	m.mu.RLock()
	pr, ok := m.prs[num]
	m.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pr)
}

func (m *GitHubMock) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	mergeable := true
	pr := &github.PullRequest{
		Number:    m.nextPR,
		Title:     req.Title,
		Body:      req.Body,
		State:     "open",
		Merged:    false,
		Mergeable: &mergeable,
		Head: github.PRRef{
			Ref: req.Head,
			SHA: "abc" + strconv.Itoa(m.nextPR) + "123",
		},
		Base: github.PRRef{
			Ref: req.Base,
		},
		HTMLURL: m.server.URL + "/owner/repo/pull/" + strconv.Itoa(m.nextPR),
	}
	m.prs[m.nextPR] = pr
	m.nextPR++

	if m.OnPRCreated != nil {
		m.OnPRCreated(pr)
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(pr)
}

func (m *GitHubMock) handleUpdatePR(w http.ResponseWriter, r *http.Request) {
	num := m.extractPRNumber(r.URL.Path)
	if num == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var req struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	pr, ok := m.prs[num]
	if ok && req.State != "" {
		pr.State = req.State
	}
	m.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pr)
}

func (m *GitHubMock) handleMergePR(w http.ResponseWriter, r *http.Request) {
	num := m.extractPRNumber(r.URL.Path)
	if num == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	m.mu.Lock()
	pr, ok := m.prs[num]
	if ok {
		pr.State = "closed"
		pr.Merged = true
		if m.OnPRMerged != nil {
			m.OnPRMerged(num)
		}
	}
	m.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"sha":     pr.Head.SHA,
		"merged":  true,
		"message": "Pull Request successfully merged",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (m *GitHubMock) handleGetCheckRuns(w http.ResponseWriter, r *http.Request) {
	sha := m.extractSHA(r.URL.Path)

	m.mu.RLock()
	checks, ok := m.checkRuns[sha]
	m.mu.RUnlock()

	if !ok {
		// Return empty checks
		checks = &github.CheckRunsResponse{TotalCount: 0, CheckRuns: []github.CheckRun{}}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(checks)
}

func (m *GitHubMock) handleGetBranch(w http.ResponseWriter, r *http.Request) {
	// Return a simple branch response
	resp := github.Branch{
		Name:   "main",
		Commit: github.BranchCommit{SHA: "mainsha123"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
