package mocks

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

func (m *GitHubMock) handleListIssues(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var issues []*github.Issue
	for _, issue := range m.issues {
		if issue.State == "open" {
			issues = append(issues, issue)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(issues)
}

func (m *GitHubMock) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	num := m.extractIssueNumber(r.URL.Path)
	if num == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	m.mu.RLock()
	issue, ok := m.issues[num]
	m.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(issue)
}

func (m *GitHubMock) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	issue := m.CreateIssue(req.Title, req.Body, req.Labels)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(issue)
}

func (m *GitHubMock) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	num := m.extractIssueNumber(r.URL.Path)
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
	issue, ok := m.issues[num]
	if ok && req.State != "" {
		issue.State = req.State
	}
	m.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(issue)
}

func (m *GitHubMock) handleAddLabel(w http.ResponseWriter, r *http.Request) {
	num := m.extractIssueNumber(r.URL.Path)
	if num == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var req struct {
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	issue, ok := m.issues[num]
	if ok {
		for _, label := range req.Labels {
			issue.Labels = append(issue.Labels, github.Label{Name: label})
			if m.OnIssueLabelAdded != nil {
				m.OnIssueLabelAdded(num, label)
			}
		}
	}
	m.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(issue.Labels)
}

func (m *GitHubMock) handleRemoveLabel(w http.ResponseWriter, r *http.Request) {
	num := m.extractIssueNumber(r.URL.Path)
	label := m.extractLabel(r.URL.Path)

	m.mu.Lock()
	issue, ok := m.issues[num]
	if ok {
		var newLabels []github.Label
		for _, l := range issue.Labels {
			if !strings.EqualFold(l.Name, label) {
				newLabels = append(newLabels, l)
			}
		}
		issue.Labels = newLabels
		if m.OnIssueLabelRemoved != nil {
			m.OnIssueLabelRemoved(num, label)
		}
	}
	m.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (m *GitHubMock) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	num := m.extractIssueNumber(r.URL.Path)

	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	commentID := m.nextComment
	m.nextComment++
	if m.OnCommentCreated != nil {
		m.OnCommentCreated(num, req.Body)
	}
	m.mu.Unlock()

	resp := map[string]interface{}{"id": commentID, "body": req.Body}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}
