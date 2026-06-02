package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewClient(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	if client == nil {
		t.Fatal("NewClient returned nil")
		return
	}
	if client.baseURL != BaseURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, BaseURL)
	}
	if client.workspaceID != testutil.FakeAsanaWorkspaceID {
		t.Errorf("client.workspaceID = %s, want %s", client.workspaceID, testutil.FakeAsanaWorkspaceID)
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	customURL := "https://custom.asana.test"
	client := NewClientWithBaseURL(customURL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	if client.baseURL != customURL {
		t.Errorf("client.baseURL = %s, want %s", client.baseURL, customURL)
	}
}

func TestNewClientWithBaseURL_TrimsTrailingSlash(t *testing.T) {
	client := NewClientWithBaseURL("https://custom.asana.test/", testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	if client.baseURL != "https://custom.asana.test" {
		t.Errorf("client.baseURL = %s, want no trailing slash", client.baseURL)
	}
}

func TestGetTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/tasks/123456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testutil.FakeAsanaAccessToken {
			t.Error("missing or incorrect Authorization header")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing Accept header")
		}

		resp := APIResponse[Task]{
			Data: Task{
				GID:   "123456",
				Name:  "Test Task",
				Notes: "Task description",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	task, err := client.GetTask(context.Background(), "123456")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task.GID != "123456" {
		t.Errorf("task.GID = %s, want 123456", task.GID)
	}
	if task.Name != "Test Task" {
		t.Errorf("task.Name = %s, want Test Task", task.Name)
	}
}

func TestGetTaskWithFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify fields parameter
		fields := r.URL.Query().Get("opt_fields")
		if fields == "" {
			t.Error("expected opt_fields query parameter")
		}

		resp := APIResponse[Task]{
			Data: Task{
				GID:  "123456",
				Name: "Test Task",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	task, err := client.GetTaskWithFields(context.Background(), "123456", []string{"gid", "name", "notes"})
	if err != nil {
		t.Fatalf("GetTaskWithFields failed: %v", err)
	}
	if task.GID != "123456" {
		t.Errorf("task.GID = %s, want 123456", task.GID)
	}
}

func TestUpdateTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data, ok := body["data"].(map[string]interface{})
		if !ok {
			t.Error("expected data wrapper in request body")
		}
		if data["completed"] != true {
			t.Error("expected completed to be true")
		}

		resp := APIResponse[Task]{
			Data: Task{
				GID:       "123456",
				Completed: true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	task, err := client.UpdateTask(context.Background(), "123456", map[string]interface{}{
		"completed": true,
	})
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}
	if !task.Completed {
		t.Error("expected task to be completed")
	}
}

func TestCompleteTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[Task]{
			Data: Task{
				GID:       "123456",
				Completed: true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	task, err := client.CompleteTask(context.Background(), "123456")
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}
	if !task.Completed {
		t.Error("expected task to be completed")
	}
}

func TestAddComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/123456/stories" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data, ok := body["data"].(map[string]interface{})
		if !ok {
			t.Error("expected data wrapper in request body")
		}
		if data["text"] != "Test comment" {
			t.Errorf("unexpected comment text: %v", data["text"])
		}

		resp := APIResponse[Story]{
			Data: Story{
				GID:  "789",
				Text: "Test comment",
				Type: "comment",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	story, err := client.AddComment(context.Background(), "123456", "Test comment")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if story.Text != "Test comment" {
		t.Errorf("story.Text = %s, want Test comment", story.Text)
	}
}

func TestAddHTMLComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data, ok := body["data"].(map[string]interface{})
		if !ok {
			t.Error("expected data wrapper in request body")
		}
		if data["html_text"] != "<b>Bold</b> text" {
			t.Errorf("unexpected html_text: %v", data["html_text"])
		}

		resp := APIResponse[Story]{
			Data: Story{
				GID:      "789",
				HTMLText: "<b>Bold</b> text",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	_, err := client.AddHTMLComment(context.Background(), "123456", "<b>Bold</b> text")
	if err != nil {
		t.Fatalf("AddHTMLComment failed: %v", err)
	}
}
