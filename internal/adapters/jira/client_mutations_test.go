package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddComment_Cloud(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/3/issue/PROJ-42/comment" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		// Cloud uses ADF format
		bodyContent, ok := body["body"].(map[string]interface{})
		if !ok {
			t.Error("expected ADF body format for Cloud")
		}
		if bodyContent["type"] != "doc" {
			t.Errorf("expected body type 'doc', got %v", bodyContent["type"])
		}

		comment := Comment{
			ID:   "10001",
			Body: "Test comment",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(comment)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@example.com", "api-token", PlatformCloud)
	_, err := client.AddComment(context.Background(), "PROJ-42", "Test comment")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
}

func TestAddComment_Server(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/2/issue/PROJ-42/comment" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		// Server uses plain text
		if body["body"] != "Test comment" {
			t.Errorf("expected plain text body for Server, got %v", body)
		}

		comment := Comment{
			ID:   "10001",
			Body: "Test comment",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(comment)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "token", PlatformServer)
	_, err := client.AddComment(context.Background(), "PROJ-42", "Test comment")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
}

func TestGetTransitions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PROJ-42/transitions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := TransitionsResponse{
			Transitions: []Transition{
				{ID: "21", Name: "Start Progress", To: Status{Name: "In Progress"}},
				{ID: "31", Name: "Done", To: Status{Name: "Done"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@example.com", "api-token", PlatformCloud)
	transitions, err := client.GetTransitions(context.Background(), "PROJ-42")
	if err != nil {
		t.Fatalf("GetTransitions failed: %v", err)
	}
	if len(transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(transitions))
	}
}

func TestTransitionIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		transition := body["transition"].(map[string]interface{})
		if transition["id"] != "21" {
			t.Errorf("expected transition id '21', got %v", transition["id"])
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@example.com", "api-token", PlatformCloud)
	err := client.TransitionIssue(context.Background(), "PROJ-42", "21")
	if err != nil {
		t.Fatalf("TransitionIssue failed: %v", err)
	}
}

func TestAddPRLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/3/issue/PROJ-42/remotelink" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body RemoteLink
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		if body.Object.URL != "https://github.com/owner/repo/pull/123" {
			t.Errorf("unexpected PR URL: %s", body.Object.URL)
		}
		if body.Object.Title != "PR #123" {
			t.Errorf("unexpected PR title: %s", body.Object.Title)
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@example.com", "api-token", PlatformCloud)
	err := client.AddPRLink(context.Background(), "PROJ-42", "https://github.com/owner/repo/pull/123", "PR #123")
	if err != nil {
		t.Fatalf("AddPRLink failed: %v", err)
	}
}

func TestSearchIssues_Cloud(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["jql"] != "labels = pilot" {
			t.Errorf("unexpected jql: %v", body["jql"])
		}
		if _, ok := body["fields"]; !ok {
			t.Error("expected fields in body")
		}

		resp := map[string]interface{}{
			"issues": []Issue{
				{ID: "10001", Key: "PROJ-1", Fields: Fields{Summary: "First"}},
				{ID: "10002", Key: "PROJ-2", Fields: Fields{Summary: "Second"}},
			},
			"nextPageToken": nil,
			"isLast":        true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@example.com", "api-token", PlatformCloud)
	issues, err := client.SearchIssues(context.Background(), "labels = pilot", 50)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0].Key != "PROJ-1" {
		t.Errorf("expected first issue PROJ-1, got %s", issues[0].Key)
	}
}

func TestSearchIssues_Server(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/2/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("jql") == "" {
			t.Error("expected jql query param")
		}

		resp := SearchResponse{
			Issues: []*Issue{{ID: "10001", Key: "PROJ-1"}},
			Total:  1,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin", "token", PlatformServer)
	issues, err := client.SearchIssues(context.Background(), "labels = pilot", 50)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}
