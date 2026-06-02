package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestAutoMerger_MergePR_DevEnvironment(t *testing.T) {
	mergeCalledWith := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			// Auto-review call
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/pulls/42/files":
			// GH-2585: size-floor gate enumerates files. Return empty list.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		case "/repos/owner/repo/pulls/42/merge":
			// Merge call
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mergeCalledWith = body["merge_method"]
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = true
	cfg.MergeMethod = github.MergeMethodSquash

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		PRURL:    "https://github.com/owner/repo/pull/42",
		HeadSHA:  "abc123",
	}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if mergeCalledWith != github.MergeMethodSquash {
		t.Errorf("merge called with method = %s, want squash", mergeCalledWith)
	}
}

func TestAutoMerger_MergePR_NonProdWithApprovalDisabled(t *testing.T) {
	// Scenario: Non-prod environment (dev/stage) with approval disabled should still auto-approve
	mergeWasCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/pulls/42/merge":
			mergeWasCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)

	approvalCfg := approval.DefaultConfig()
	approvalCfg.Enabled = true
	approvalCfg.PreMerge.Enabled = false
	approvalMgr := approval.NewManager(approvalCfg)

	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = true

	merger := NewAutoMerger(ghClient, approvalMgr, nil, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		PRURL:    "https://github.com/owner/repo/pull/42",
	}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if !mergeWasCalled {
		t.Error("merge should have been called in dev when approval stage is disabled")
	}
}

func TestAutoMerger_MergePR_DefaultMergeMethod(t *testing.T) {
	mergeCalledWith := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/merge":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mergeCalledWith = body["merge_method"]
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.MergeMethod = "" // Empty - should default to squash

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	prState := &PRState{PRNumber: 42}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if mergeCalledWith != github.MergeMethodSquash {
		t.Errorf("merge method = %s, want squash (default)", mergeCalledWith)
	}
}

func TestAutoMerger_MergePR_MergeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/pulls/42/merge":
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message": "Pull request is not mergeable"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = true

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	prState := &PRState{PRNumber: 42}

	err := merger.MergePR(context.Background(), prState)
	if err == nil {
		t.Error("MergePR() should return error when merge fails")
	}
}

func TestAutoMerger_MergePR_AutoReviewFailureContinues(t *testing.T) {
	mergeWasCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			// Auto-review fails (e.g., already reviewed)
			w.WriteHeader(http.StatusUnprocessableEntity)
		case "/repos/owner/repo/pulls/42/merge":
			// Merge should still be attempted
			mergeWasCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	cfg.AutoReview = true

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	prState := &PRState{PRNumber: 42}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if !mergeWasCalled {
		t.Error("merge should still be called when auto-review fails")
	}
}

func TestAutoMerger_MergePR_StageEnvironment(t *testing.T) {
	mergeWasCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/42/reviews":
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/pulls/42/merge":
			mergeWasCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvStage
	cfg.AutoReview = true

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	prState := &PRState{PRNumber: 42}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if !mergeWasCalled {
		t.Error("merge should be called for stage environment")
	}
}

func TestAutoMerger_MergePR_AllMergeMethods(t *testing.T) {
	methods := []string{
		github.MergeMethodMerge,
		github.MergeMethodSquash,
		github.MergeMethodRebase,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			capturedMethod := ""

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls/42/merge" {
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					capturedMethod = body["merge_method"]
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.Environment = EnvDev
			cfg.AutoReview = false
			cfg.MergeMethod = method

			merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

			err := merger.MergePR(context.Background(), &PRState{PRNumber: 42})
			if err != nil {
				t.Errorf("MergePR() error = %v", err)
			}

			if capturedMethod != method {
				t.Errorf("merge method = %s, want %s", capturedMethod, method)
			}
		})
	}
}
