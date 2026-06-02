package azuredevops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCreatePullRequest(t *testing.T) {
	expectedPR := PullRequest{
		PullRequestID: 42,
		Title:         "Test PR",
		Description:   "Test description",
		Status:        PRStateActive,
		SourceRefName: "refs/heads/feature",
		TargetRefName: "refs/heads/main",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var input map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		// Verify refs are in full format
		if input["sourceRefName"] != "refs/heads/feature" {
			t.Errorf("expected source ref refs/heads/feature, got %s", input["sourceRefName"])
		}
		if input["targetRefName"] != "refs/heads/main" {
			t.Errorf("expected target ref refs/heads/main, got %s", input["targetRefName"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expectedPR)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	ctx := context.Background()

	pr, err := client.CreatePullRequest(ctx, &PullRequestInput{
		Title:         "Test PR",
		Description:   "Test description",
		SourceRefName: "feature", // Should be expanded to refs/heads/feature
		TargetRefName: "main",    // Should be expanded to refs/heads/main
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pr.PullRequestID != 42 {
		t.Errorf("expected PR ID 42, got %d", pr.PullRequestID)
	}
}

func TestGetPullRequest(t *testing.T) {
	expectedPR := PullRequest{
		PullRequestID: 42,
		Title:         "Test PR",
		Status:        PRStateActive,
		MergeStatus:   MergeStatusQueued,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expectedPR)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	ctx := context.Background()

	pr, err := client.GetPullRequest(ctx, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pr.PullRequestID != 42 {
		t.Errorf("expected PR ID 42, got %d", pr.PullRequestID)
	}
	if pr.Status != PRStateActive {
		t.Errorf("expected status active, got %s", pr.Status)
	}
}
