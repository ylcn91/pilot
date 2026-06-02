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

func TestLinkPR(t *testing.T) {
	requestCount := 0
	var capturedComment string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if strings.Contains(r.URL.Path, "/attachments") {
			// Attachment request
			resp := APIResponse[Attachment]{
				Data: Attachment{GID: "attach-1", Name: "PR #42"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(r.URL.Path, "/stories") {
			// Comment request
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
			return
		}

		t.Errorf("unexpected request path: %s", r.URL.Path)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.LinkPR(context.Background(), "123456", 42, "https://github.com/owner/repo/pull/42")
	if err != nil {
		t.Fatalf("LinkPR failed: %v", err)
	}

	if requestCount < 2 {
		t.Errorf("expected at least 2 requests (attachment + comment), got %d", requestCount)
	}

	if !strings.Contains(capturedComment, "PR #42") {
		t.Errorf("comment should mention PR number, got: %s", capturedComment)
	}
	if !strings.Contains(capturedComment, "github.com/owner/repo/pull/42") {
		t.Errorf("comment should contain PR URL, got: %s", capturedComment)
	}
}

func TestRemovePilotTag(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if strings.Contains(r.URL.Path, "/tags") && r.Method == http.MethodGet {
			// Get workspace tags
			resp := PagedResponse[Tag]{
				Data: []Tag{
					{GID: "pilot-tag-1", Name: "pilot"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(r.URL.Path, "/removeTag") {
			// Remove tag request
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.RemovePilotTag(context.Background(), "123456")
	if err != nil {
		t.Fatalf("RemovePilotTag failed: %v", err)
	}

	if requestCount != 2 {
		t.Errorf("expected 2 requests (get tags + remove tag), got %d", requestCount)
	}
}

func TestRemovePilotTag_CachedGID(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if strings.Contains(r.URL.Path, "/removeTag") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")
	notifier.pilotTagGID = "cached-tag-gid" // Pre-cache the GID

	err := notifier.RemovePilotTag(context.Background(), "123456")
	if err != nil {
		t.Fatalf("RemovePilotTag failed: %v", err)
	}

	// Should only make the remove request, not the get tags request
	if requestCount != 1 {
		t.Errorf("expected 1 request (remove tag only), got %d", requestCount)
	}
}

func TestAddPilotTag(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if strings.Contains(r.URL.Path, "/workspaces/") && strings.Contains(r.URL.Path, "/tags") {
			// Get workspace tags
			resp := PagedResponse[Tag]{
				Data: []Tag{
					{GID: "pilot-tag-1", Name: "pilot"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(r.URL.Path, "/addTag") {
			// Add tag request
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.AddPilotTag(context.Background(), "123456")
	if err != nil {
		t.Fatalf("AddPilotTag failed: %v", err)
	}

	if requestCount != 2 {
		t.Errorf("expected 2 requests (get tags + add tag), got %d", requestCount)
	}
}

func TestAddPilotTag_CreateNew(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if strings.Contains(r.URL.Path, "/workspaces/") && strings.Contains(r.URL.Path, "/tags") {
			// Get workspace tags - return empty (tag doesn't exist)
			resp := PagedResponse[Tag]{
				Data: []Tag{},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.URL.Path == "/tags" && r.Method == http.MethodPost {
			// Create tag request
			resp := APIResponse[Tag]{
				Data: Tag{GID: "new-tag-1", Name: "pilot"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.Contains(r.URL.Path, "/addTag") {
			// Add tag request
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.AddPilotTag(context.Background(), "123456")
	if err != nil {
		t.Fatalf("AddPilotTag failed: %v", err)
	}

	// Should make: get tags, create tag, add tag
	if requestCount != 3 {
		t.Errorf("expected 3 requests, got %d", requestCount)
	}
}

func TestLinkPR_AttachmentFailsFallsBackToComment(t *testing.T) {
	var capturedComment string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/attachments") {
			// Attachment fails
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"not authorized"}]}`))
			return
		}

		if strings.Contains(r.URL.Path, "/stories") {
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
			return
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.LinkPR(context.Background(), "123456", 42, "https://github.com/owner/repo/pull/42")
	if err != nil {
		t.Fatalf("LinkPR should succeed even if attachment fails: %v", err)
	}

	if !strings.Contains(capturedComment, "PR #42") {
		t.Errorf("comment should mention PR number, got: %s", capturedComment)
	}
}

func TestRemovePilotTag_TagNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty tags list
		resp := PagedResponse[Tag]{
			Data: []Tag{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	notifier := NewNotifier(client, "pilot")

	err := notifier.RemovePilotTag(context.Background(), "123456")
	if err != nil {
		t.Fatalf("RemovePilotTag should succeed when tag not found: %v", err)
	}
}
