package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewAutoMerger(t *testing.T) {
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	approvalMgr := approval.NewManager(nil)
	cfg := DefaultConfig()

	merger := NewAutoMerger(ghClient, approvalMgr, nil, "owner", "repo", cfg)

	if merger == nil {
		t.Fatal("NewAutoMerger returned nil")
	}
	if merger.owner != "owner" {
		t.Errorf("owner = %s, want owner", merger.owner)
	}
	if merger.repo != "repo" {
		t.Errorf("repo = %s, want repo", merger.repo)
	}
}

func TestAutoMerger_ShouldWaitForCI(t *testing.T) {
	// All environments now wait for CI to prevent broken code from merging
	tests := []struct {
		name     string
		env      Environment
		wantWait bool
	}{
		{"dev - wait for CI", EnvDev, true},
		{"stage - wait for CI", EnvStage, true},
		{"prod - wait for CI", EnvProd, true},
	}

	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)
			got := merger.ShouldWaitForCI(tt.env)
			if got != tt.wantWait {
				t.Errorf("ShouldWaitForCI(%s) = %v, want %v", tt.env, got, tt.wantWait)
			}
		})
	}
}

func TestAutoMerger_CanMerge(t *testing.T) {
	tests := []struct {
		name       string
		pr         github.PullRequest
		statusCode int
		canMerge   bool
		reason     string
		wantErr    bool
	}{
		{
			name: "can merge - open and mergeable",
			pr: github.PullRequest{
				Number:    42,
				State:     "open",
				Merged:    false,
				Mergeable: boolPtr(true),
			},
			statusCode: http.StatusOK,
			canMerge:   true,
			reason:     "",
			wantErr:    false,
		},
		{
			name: "cannot merge - already merged",
			pr: github.PullRequest{
				Number:    42,
				State:     "closed",
				Merged:    true,
				Mergeable: boolPtr(false),
			},
			statusCode: http.StatusOK,
			canMerge:   false,
			reason:     "already merged",
			wantErr:    false,
		},
		{
			name: "cannot merge - closed",
			pr: github.PullRequest{
				Number:    42,
				State:     "closed",
				Merged:    false,
				Mergeable: boolPtr(true),
			},
			statusCode: http.StatusOK,
			canMerge:   false,
			reason:     "PR is closed",
			wantErr:    false,
		},
		{
			name: "cannot merge - conflicts",
			pr: github.PullRequest{
				Number:    42,
				State:     "open",
				Merged:    false,
				Mergeable: boolPtr(false),
			},
			statusCode: http.StatusOK,
			canMerge:   false,
			reason:     "merge conflicts",
			wantErr:    false,
		},
		{
			name: "can merge - mergeable nil (unknown)",
			pr: github.PullRequest{
				Number:    42,
				State:     "open",
				Merged:    false,
				Mergeable: nil,
			},
			statusCode: http.StatusOK,
			canMerge:   true,
			reason:     "",
			wantErr:    false,
		},
		{
			name:       "error - not found",
			statusCode: http.StatusNotFound,
			canMerge:   false,
			reason:     "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/pulls/42" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					_ = json.NewEncoder(w).Encode(tt.pr)
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

			canMerge, reason, err := merger.CanMerge(context.Background(), 42)

			if (err != nil) != tt.wantErr {
				t.Errorf("CanMerge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if canMerge != tt.canMerge {
				t.Errorf("CanMerge() canMerge = %v, want %v", canMerge, tt.canMerge)
			}
			if reason != tt.reason {
				t.Errorf("CanMerge() reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

func TestAutoMerger_ApprovePR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls/42/reviews" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body["event"] != github.ReviewEventApprove {
			t.Errorf("expected APPROVE event, got %s", body["event"])
		}
		if body["body"] != "Auto-approved by Pilot autopilot" {
			t.Errorf("unexpected review body: %s", body["body"])
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	err := merger.approvePR(context.Background(), 42)
	if err != nil {
		t.Errorf("approvePR() error = %v", err)
	}
}

func TestEnvironmentBehaviorMatrix(t *testing.T) {
	// Verify the environment behavior matrix
	// All environments now wait for CI to prevent broken code from merging
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	tests := []struct {
		env       Environment
		waitForCI bool // all environments now wait for CI
	}{
		{EnvDev, true},
		{EnvStage, true},
		{EnvProd, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

			shouldWait := merger.ShouldWaitForCI(tt.env)
			if shouldWait != tt.waitForCI {
				t.Errorf("ShouldWaitForCI(%s) = %v, want %v", tt.env, shouldWait, tt.waitForCI)
			}
		})
	}
}

func TestAutoMerger_CanMerge_IntegrationScenarios(t *testing.T) {
	// Test real-world PR state combinations
	tests := []struct {
		name     string
		state    string
		merged   bool
		canMerge bool
	}{
		{"open PR", "open", false, true},
		{"merged PR", "closed", true, false},
		{"closed without merge", "closed", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pr := github.PullRequest{
					Number:    42,
					State:     tt.state,
					Merged:    tt.merged,
					Mergeable: boolPtr(true),
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(pr)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

			canMerge, _, err := merger.CanMerge(context.Background(), 42)
			if err != nil {
				t.Errorf("CanMerge() error = %v", err)
			}
			if canMerge != tt.canMerge {
				t.Errorf("CanMerge() = %v, want %v", canMerge, tt.canMerge)
			}
		})
	}
}

func TestAutoMerger_WithApprovalTimeout(t *testing.T) {
	// Verify that approval timeout from config is accessible
	cfg := DefaultConfig()
	cfg.ApprovalTimeout = 2 * time.Hour

	ghClient := github.NewClient(testutil.FakeGitHubToken)
	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	if merger.config.ApprovalTimeout != 2*time.Hour {
		t.Errorf("ApprovalTimeout = %v, want 2h", merger.config.ApprovalTimeout)
	}
}

func TestAutoMerger_PostMisconfigComment_Idempotent(t *testing.T) {
	// First call: no existing comments → comment is posted.
	// Second call: marker present in comment list → posting is skipped.
	postCount := 0
	listCallCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/repos/owner/repo/issues/42/comments":
			listCallCount++
			if postCount == 0 {
				// No comments yet.
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("[]"))
			} else {
				// Return the previously posted comment with the marker.
				resp := []map[string]interface{}{
					{"id": 1, "body": misconfigCommentMarker + "\nsome text"},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}
		case r.Method == http.MethodPost && path == "/repos/owner/repo/issues/42/comments":
			postCount++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1,"body":"posted"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)
	prState := &PRState{PRNumber: 42}

	// First call — should post the comment.
	merger.postMisconfigComment(context.Background(), prState)
	if postCount != 1 {
		t.Errorf("first call: postCount = %d, want 1", postCount)
	}

	// Second call — marker in list, should NOT post again.
	merger.postMisconfigComment(context.Background(), prState)
	if postCount != 1 {
		t.Errorf("second call: postCount = %d, want 1 (idempotent)", postCount)
	}
	if listCallCount != 2 {
		t.Errorf("listCallCount = %d, want 2", listCallCount)
	}
}
