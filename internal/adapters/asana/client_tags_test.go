package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestAddTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/123456/addTag" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data := body["data"].(map[string]interface{})
		if data["tag"] != "999" {
			t.Errorf("unexpected tag GID: %v", data["tag"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	err := client.AddTag(context.Background(), "123456", "999")
	if err != nil {
		t.Fatalf("AddTag failed: %v", err)
	}
}

func TestRemoveTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/123456/removeTag" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	err := client.RemoveTag(context.Background(), "123456", "999")
	if err != nil {
		t.Fatalf("RemoveTag failed: %v", err)
	}
}

func TestGetWorkspaceTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/workspaces/" + testutil.FakeAsanaWorkspaceID + "/tags"
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: %s, want %s", r.URL.Path, expectedPath)
		}

		resp := PagedResponse[Tag]{
			Data: []Tag{
				{GID: "1", Name: "pilot"},
				{GID: "2", Name: "urgent"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	tags, err := client.GetWorkspaceTags(context.Background())
	if err != nil {
		t.Fatalf("GetWorkspaceTags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}
}

func TestFindTagByName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := PagedResponse[Tag]{
			Data: []Tag{
				{GID: "1", Name: "pilot"},
				{GID: "2", Name: "urgent"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)

	// Test case-insensitive match
	tag, err := client.FindTagByName(context.Background(), "PILOT")
	if err != nil {
		t.Fatalf("FindTagByName failed: %v", err)
	}
	if tag == nil {
		t.Fatal("expected to find tag")
		return
	}
	if tag.GID != "1" {
		t.Errorf("tag.GID = %s, want 1", tag.GID)
	}
}

func TestFindTagByName_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := PagedResponse[Tag]{
			Data: []Tag{
				{GID: "1", Name: "other"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	tag, err := client.FindTagByName(context.Background(), "pilot")
	if err != nil {
		t.Fatalf("FindTagByName failed: %v", err)
	}
	if tag != nil {
		t.Error("expected nil when tag not found")
	}
}

func TestCreateTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data := body["data"].(map[string]interface{})
		if data["name"] != "pilot" {
			t.Errorf("unexpected tag name: %v", data["name"])
		}
		if data["workspace"] != testutil.FakeAsanaWorkspaceID {
			t.Errorf("unexpected workspace: %v", data["workspace"])
		}

		resp := APIResponse[Tag]{
			Data: Tag{
				GID:  "999",
				Name: "pilot",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	tag, err := client.CreateTag(context.Background(), "pilot")
	if err != nil {
		t.Fatalf("CreateTag failed: %v", err)
	}
	if tag.Name != "pilot" {
		t.Errorf("tag.Name = %s, want pilot", tag.Name)
	}
}

func TestAddAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/123456/attachments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		data := body["data"].(map[string]interface{})
		if data["resource_subtype"] != "external" {
			t.Errorf("unexpected resource_subtype: %v", data["resource_subtype"])
		}
		if data["url"] != "https://github.com/owner/repo/pull/123" {
			t.Errorf("unexpected url: %v", data["url"])
		}

		resp := APIResponse[Attachment]{
			Data: Attachment{
				GID:     "attach-1",
				Name:    "PR #123",
				ViewURL: "https://github.com/owner/repo/pull/123",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	attachment, err := client.AddAttachment(context.Background(), "123456", "https://github.com/owner/repo/pull/123", "PR #123")
	if err != nil {
		t.Fatalf("AddAttachment failed: %v", err)
	}
	if attachment.Name != "PR #123" {
		t.Errorf("attachment.Name = %s, want PR #123", attachment.Name)
	}
}

func TestGetWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/workspaces/" + testutil.FakeAsanaWorkspaceID
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: %s, want %s", r.URL.Path, expectedPath)
		}

		resp := APIResponse[Workspace]{
			Data: Workspace{
				GID:  testutil.FakeAsanaWorkspaceID,
				Name: "Test Workspace",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	workspace, err := client.GetWorkspace(context.Background())
	if err != nil {
		t.Fatalf("GetWorkspace failed: %v", err)
	}
	if workspace.Name != "Test Workspace" {
		t.Errorf("workspace.Name = %s, want Test Workspace", workspace.Name)
	}
}

func TestPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[Workspace]{
			Data: Workspace{
				GID:  testutil.FakeAsanaWorkspaceID,
				Name: "Test Workspace",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestDoRequest_ErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response:   `{"data": {"gid": "1"}}`,
			wantErr:    false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   `{"errors": [{"message": "Not found"}]}`,
			wantErr:    true,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   `{"errors": [{"message": "Not Authorized"}]}`,
			wantErr:    true,
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			response:   `{"errors": [{"message": "Rate limit exceeded"}]}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
			_, err := client.GetTask(context.Background(), "123")

			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// Integration test helper - verifies client method signatures compile
func TestClientMethodSignatures(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	ctx := context.Background()

	// These won't actually work without a real API, but verify the signatures compile
	var err error

	_, err = client.GetTask(ctx, "123")
	_ = err

	_, err = client.GetTaskWithFields(ctx, "123", []string{"gid", "name"})
	_ = err

	_, err = client.UpdateTask(ctx, "123", map[string]interface{}{"completed": true})
	_ = err

	_, err = client.CompleteTask(ctx, "123")
	_ = err

	_, err = client.AddComment(ctx, "123", "comment")
	_ = err

	_, err = client.AddHTMLComment(ctx, "123", "<b>html</b>")
	_ = err

	_, err = client.GetTaskStories(ctx, "123")
	_ = err

	err = client.AddTag(ctx, "123", "456")
	_ = err

	err = client.RemoveTag(ctx, "123", "456")
	_ = err

	_, err = client.GetWorkspaceTags(ctx)
	_ = err

	_, err = client.FindTagByName(ctx, "pilot")
	_ = err

	_, err = client.CreateTag(ctx, "newtag")
	_ = err

	_, err = client.AddAttachment(ctx, "123", "https://example.com", "Link")
	_ = err

	_, err = client.GetProject(ctx, "proj-1")
	_ = err

	_, err = client.GetWorkspace(ctx)
	_ = err

	_, err = client.SearchTasks(ctx, "query")
	_ = err

	_, err = client.GetTasksByTag(ctx, "tag-1")
	_ = err

	err = client.Ping(ctx)
	_ = err
}
