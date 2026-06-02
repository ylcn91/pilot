package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCreateMergeRequest(t *testing.T) {
	tests := []struct {
		name       string
		input      *MergeRequestInput
		statusCode int
		wantErr    bool
	}{
		{
			name: "success",
			input: &MergeRequestInput{
				Title:        "Add new feature",
				Description:  "This MR adds a new feature",
				SourceBranch: "feature/new-feature",
				TargetBranch: "main",
			},
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name: "unprocessable entity - branch doesn't exist",
			input: &MergeRequestInput{
				Title:        "Add new feature",
				SourceBranch: "nonexistent-branch",
				TargetBranch: "main",
			},
			statusCode: http.StatusUnprocessableEntity,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/merge_requests") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body MergeRequestInput
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body.Title != tt.input.Title {
					t.Errorf("unexpected title: %s", body.Title)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode < 300 {
					result := MergeRequest{
						ID:           11111,
						IID:          42,
						Title:        body.Title,
						SourceBranch: body.SourceBranch,
						TargetBranch: body.TargetBranch,
						State:        MRStateOpened,
						WebURL:       "https://gitlab.com/namespace/project/-/merge_requests/42",
					}
					_ = json.NewEncoder(w).Encode(result)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			result, err := client.CreateMergeRequest(context.Background(), tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateMergeRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.IID != 42 {
				t.Errorf("result.IID = %d, want 42", result.IID)
			}
		})
	}
}

func TestCreatePR_PRCreatorInterface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body MergeRequestInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body.SourceBranch != "pilot/GL-1" {
			t.Errorf("source_branch = %q, want %q", body.SourceBranch, "pilot/GL-1")
		}
		if body.TargetBranch != "main" {
			t.Errorf("target_branch = %q, want %q", body.TargetBranch, "main")
		}
		if !body.RemoveSourceBranch {
			t.Error("remove_source_branch should be true")
		}
		w.WriteHeader(http.StatusCreated)
		result := MergeRequest{
			IID:          7,
			Title:        body.Title,
			SourceBranch: body.SourceBranch,
			TargetBranch: body.TargetBranch,
			State:        MRStateOpened,
			WebURL:       "https://gitlab.com/ns/proj/-/merge_requests/7",
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeGitLabToken, "ns/proj", server.URL)
	url, err := client.CreatePR(context.Background(), "pilot/GL-1", "main", "GL-1: Fix bug", "## Summary\n\nFix it")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if url != "https://gitlab.com/ns/proj/-/merge_requests/7" {
		t.Errorf("CreatePR() url = %q, want gitlab MR URL", url)
	}
}

func TestGetMergeRequest(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			response: MergeRequest{
				ID:           11111,
				IID:          42,
				Title:        "Test MR",
				SourceBranch: "feature-branch",
				TargetBranch: "main",
				State:        MRStateOpened,
				WebURL:       "https://gitlab.com/namespace/project/-/merge_requests/42",
			},
			wantErr: false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   map[string]string{"message": "404 Merge Request Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/merge_requests/42") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			result, err := client.GetMergeRequest(context.Background(), 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetMergeRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.IID != 42 {
				t.Errorf("result.IID = %d, want 42", result.IID)
			}
		})
	}
}

func TestMergeMergeRequest(t *testing.T) {
	tests := []struct {
		name       string
		squash     bool
		statusCode int
		wantErr    bool
	}{
		{
			name:       "success - no squash",
			squash:     false,
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "success - with squash",
			squash:     true,
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not mergeable - conflicts",
			squash:     false,
			statusCode: http.StatusMethodNotAllowed,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT, got %s", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/merge_requests/42/merge") {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}

				if body["squash"] != tt.squash {
					t.Errorf("unexpected squash: %v, want %v", body["squash"], tt.squash)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					mr := MergeRequest{
						IID:   42,
						State: MRStateMerged,
					}
					_ = json.NewEncoder(w).Encode(mr)
				}
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", server.URL)
			_, err := client.MergeMergeRequest(context.Background(), 42, tt.squash)

			if (err != nil) != tt.wantErr {
				t.Errorf("MergeMergeRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
