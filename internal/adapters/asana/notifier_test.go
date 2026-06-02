package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewNotifier(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	if notifier == nil {
		t.Fatal("NewNotifier returned nil")
		return
	}
	if notifier.pilotTag != "pilot" {
		t.Errorf("notifier.pilotTag = %s, want pilot", notifier.pilotTag)
	}
}

func TestNotifyTaskStarted(t *testing.T) {
	var capturedComment string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data := body["data"].(map[string]interface{})
		capturedComment = data["text"].(string)

		resp := APIResponse[Story]{
			Data: Story{
				GID:  "story-1",
				Text: capturedComment,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskStarted(context.Background(), "123456", "PILOT-001")
	if err != nil {
		t.Fatalf("NotifyTaskStarted failed: %v", err)
	}

	if !strings.Contains(capturedComment, "Pilot started working") {
		t.Errorf("comment should mention Pilot started, got: %s", capturedComment)
	}
	if !strings.Contains(capturedComment, "PILOT-001") {
		t.Errorf("comment should contain task ID, got: %s", capturedComment)
	}
}

func TestNotifyProgress(t *testing.T) {
	tests := []struct {
		phase     string
		wantEmoji string
	}{
		{"exploring", "🔍"},
		{"research", "🔍"},
		{"implementing", "🔨"},
		{"impl", "🔨"},
		{"testing", "🧪"},
		{"verify", "🧪"},
		{"committing", "📝"},
		{"reviewing", "👀"},
		{"unknown", "⏳"},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			var capturedComment string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				data := body["data"].(map[string]interface{})
				capturedComment = data["text"].(string)

				resp := APIResponse[Story]{
					Data: Story{GID: "story-1", Text: capturedComment},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
			notifier := NewNotifier(client, "pilot")

			err := notifier.NotifyProgress(context.Background(), "123456", tt.phase, "Progress details")
			if err != nil {
				t.Fatalf("NotifyProgress failed: %v", err)
			}

			if !strings.Contains(capturedComment, tt.wantEmoji) {
				t.Errorf("comment should contain emoji %s, got: %s", tt.wantEmoji, capturedComment)
			}
			if !strings.Contains(capturedComment, tt.phase) {
				t.Errorf("comment should contain phase %s, got: %s", tt.phase, capturedComment)
			}
		})
	}
}

func TestNotifyTaskCompleted(t *testing.T) {
	var capturedComment string
	var completeCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/tasks/") {
			// CompleteTask call
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			data := body["data"].(map[string]interface{})
			if data["completed"] == true {
				completeCalled = true
			}
			resp := APIResponse[Task]{
				Data: Task{GID: "123456", Completed: true},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Comment (story) request
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		data := body["data"].(map[string]interface{})
		capturedComment = data["text"].(string)

		resp := APIResponse[Story]{
			Data: Story{GID: "story-1", Text: capturedComment},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskCompleted(context.Background(), "123456", "https://github.com/owner/repo/pull/42", "Added feature X")
	if err != nil {
		t.Fatalf("NotifyTaskCompleted failed: %v", err)
	}

	if !strings.Contains(capturedComment, "Pilot completed") {
		t.Errorf("comment should mention completion, got: %s", capturedComment)
	}
	if !strings.Contains(capturedComment, "github.com/owner/repo/pull/42") {
		t.Errorf("comment should contain PR URL, got: %s", capturedComment)
	}
	if !strings.Contains(capturedComment, "Added feature X") {
		t.Errorf("comment should contain summary, got: %s", capturedComment)
	}
	if !completeCalled {
		t.Error("expected CompleteTask to be called on success path")
	}
}

func TestNotifyTaskCompleted_NoPR(t *testing.T) {
	var capturedComment string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// CompleteTask call
			resp := APIResponse[Task]{
				Data: Task{GID: "123456", Completed: true},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		data := body["data"].(map[string]interface{})
		capturedComment = data["text"].(string)

		resp := APIResponse[Story]{
			Data: Story{GID: "story-1", Text: capturedComment},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskCompleted(context.Background(), "123456", "", "")
	if err != nil {
		t.Fatalf("NotifyTaskCompleted failed: %v", err)
	}

	if !strings.Contains(capturedComment, "Pilot completed") {
		t.Errorf("comment should mention completion, got: %s", capturedComment)
	}
}

func TestNotifyTaskCompleted_CompleteTaskErrorDoesNotFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// CompleteTask fails
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"not authorized"}]}`))
			return
		}

		// Comment succeeds
		resp := APIResponse[Story]{
			Data: Story{GID: "story-1", Text: "ok"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	// Should NOT return an error even though CompleteTask fails
	err := notifier.NotifyTaskCompleted(context.Background(), "123456", "https://github.com/pr/1", "summary")
	if err != nil {
		t.Fatalf("NotifyTaskCompleted should succeed even if CompleteTask fails, got: %v", err)
	}
}

func TestNotifier_CompleteTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data := body["data"].(map[string]interface{})
		if data["completed"] != true {
			t.Error("expected completed to be true")
		}

		resp := APIResponse[Task]{
			Data: Task{GID: "123456", Completed: true},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.CompleteTask(context.Background(), "123456")
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}
}

func TestNotifyTaskFailed(t *testing.T) {
	var capturedComment string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		data := body["data"].(map[string]interface{})
		capturedComment = data["text"].(string)

		resp := APIResponse[Story]{
			Data: Story{GID: "story-1", Text: capturedComment},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.NotifyTaskFailed(context.Background(), "123456", "Build failed with errors")
	if err != nil {
		t.Fatalf("NotifyTaskFailed failed: %v", err)
	}

	if !strings.Contains(capturedComment, "could not complete") {
		t.Errorf("comment should mention failure, got: %s", capturedComment)
	}
	if !strings.Contains(capturedComment, "Build failed with errors") {
		t.Errorf("comment should contain reason, got: %s", capturedComment)
	}
}
