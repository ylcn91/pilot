package plane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestAddComment(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v1/workspaces/ws/projects/proj-1/work-items/wi-42/comments/"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, wantPath)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
	err := c.AddComment(context.Background(), "ws", "proj-1", "wi-42", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["comment_html"] != "<p>Hello</p>" {
		t.Errorf("expected comment_html in body, got %v", gotBody)
	}
}

func TestListComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/workspaces/ws/projects/proj-1/work-items/wi-42/comments/"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, wantPath)
		}
		resp := commentsResponse{Results: []Comment{
			{ID: "c-1", CommentHTML: "<p>First</p>", ExternalSource: "pilot", ExternalID: "pilot-pr-1-wi-42"},
			{ID: "c-2", CommentHTML: "<p>Second</p>"},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
	comments, err := c.ListComments(context.Background(), "ws", "proj-1", "wi-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ExternalID != "pilot-pr-1-wi-42" {
		t.Errorf("expected external_id 'pilot-pr-1-wi-42', got %s", comments[0].ExternalID)
	}
}

func TestAddCommentWithTracking(t *testing.T) {
	t.Run("posts new comment when no duplicate exists", func(t *testing.T) {
		calls := 0
		var postedBody map[string]string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == http.MethodGet {
				// ListComments returns empty
				resp := commentsResponse{Results: []Comment{}}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			if r.Method == http.MethodPost {
				_ = json.NewDecoder(r.Body).Decode(&postedBody)
				w.WriteHeader(http.StatusOK)
				return
			}
		}))
		defer srv.Close()

		c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
		err := c.AddCommentWithTracking(
			context.Background(), "ws", "proj-1", "wi-42",
			"<p>PR #5</p>", "pilot", "pilot-pr-5-wi-42",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if postedBody["comment_html"] != "<p>PR #5</p>" {
			t.Errorf("expected comment_html '<p>PR #5</p>', got %s", postedBody["comment_html"])
		}
		if postedBody["external_source"] != "pilot" {
			t.Errorf("expected external_source 'pilot', got %s", postedBody["external_source"])
		}
		if postedBody["external_id"] != "pilot-pr-5-wi-42" {
			t.Errorf("expected external_id 'pilot-pr-5-wi-42', got %s", postedBody["external_id"])
		}
		if postedBody["access"] != "INTERNAL" {
			t.Errorf("expected access 'INTERNAL', got %s", postedBody["access"])
		}
	})

	t.Run("skips posting when duplicate external_id exists", func(t *testing.T) {
		postCalled := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				// Return existing comment with matching external_id
				resp := commentsResponse{Results: []Comment{
					{ID: "c-existing", CommentHTML: "<p>PR #5</p>", ExternalSource: "pilot", ExternalID: "pilot-pr-5-wi-42"},
				}}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			if r.Method == http.MethodPost {
				postCalled = true
				w.WriteHeader(http.StatusOK)
				return
			}
		}))
		defer srv.Close()

		c := NewClient(srv.URL, testutil.FakePlaneAPIKey)
		err := c.AddCommentWithTracking(
			context.Background(), "ws", "proj-1", "wi-42",
			"<p>PR #5</p>", "pilot", "pilot-pr-5-wi-42",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if postCalled {
			t.Error("expected POST to be skipped for duplicate external_id")
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("expected default config to be disabled")
	}
	if cfg.BaseURL != "https://api.plane.so" {
		t.Errorf("expected default BaseURL https://api.plane.so, got %s", cfg.BaseURL)
	}
	if cfg.PilotLabel != "pilot" {
		t.Errorf("expected default PilotLabel pilot, got %s", cfg.PilotLabel)
	}
}
