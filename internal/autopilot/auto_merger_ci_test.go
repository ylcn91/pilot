package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestAutoMerger_VerifyCIBeforeMerge(t *testing.T) {
	tests := []struct {
		name        string
		ciStatus    CIStatus
		wantErr     bool
		errContains string
	}{
		{
			name:     "CI success - verification passes",
			ciStatus: CISuccess,
			wantErr:  false,
		},
		{
			name:        "CI failure - verification fails",
			ciStatus:    CIFailure,
			wantErr:     true,
			errContains: "CI checks failing",
		},
		{
			name:        "CI pending - verification fails",
			ciStatus:    CIPending,
			wantErr:     true,
			errContains: "CI checks still pending",
		},
		{
			name:        "CI running - verification fails",
			ciStatus:    CIRunning,
			wantErr:     true,
			errContains: "CI checks still pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock server that returns check runs with the desired status
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/commits/abc123def/check-runs" {
					checkStatus := github.CheckRunCompleted
					conclusion := github.ConclusionSuccess

					switch tt.ciStatus {
					case CISuccess:
						checkStatus = github.CheckRunCompleted
						conclusion = github.ConclusionSuccess
					case CIFailure:
						checkStatus = github.CheckRunCompleted
						conclusion = github.ConclusionFailure
					case CIPending:
						checkStatus = github.CheckRunQueued
						conclusion = ""
					case CIRunning:
						checkStatus = github.CheckRunInProgress
						conclusion = ""
					}

					response := github.CheckRunsResponse{
						TotalCount: 1,
						CheckRuns: []github.CheckRun{
							{
								Name:       "build",
								Status:     checkStatus,
								Conclusion: conclusion,
							},
						},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(response)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.RequiredChecks = []string{} // Check all runs

			ciMonitor := NewCIMonitor(ghClient, "owner", "repo", cfg)
			merger := NewAutoMerger(ghClient, nil, ciMonitor, "owner", "repo", cfg)

			prState := &PRState{
				PRNumber: 42,
				HeadSHA:  "abc123def",
			}

			err := merger.verifyCIBeforeMerge(context.Background(), prState)

			if (err != nil) != tt.wantErr {
				t.Errorf("verifyCIBeforeMerge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsStr(err.Error(), tt.errContains) {
					t.Errorf("verifyCIBeforeMerge() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestAutoMerger_VerifyCIBeforeMerge_NoCIMonitor(t *testing.T) {
	// When CI monitor is nil, verification should be skipped (no error)
	ghClient := github.NewClient(testutil.FakeGitHubToken)
	cfg := DefaultConfig()

	merger := NewAutoMerger(ghClient, nil, nil, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc123",
	}

	err := merger.verifyCIBeforeMerge(context.Background(), prState)
	if err != nil {
		t.Errorf("verifyCIBeforeMerge() with nil CIMonitor should not error, got %v", err)
	}
}

func TestAutoMerger_MergePR_StageWithCIVerification(t *testing.T) {
	// Stage environment should verify CI before merge
	mergeWasCalled := false
	ciCheckCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc123def/check-runs":
			ciCheckCalled = true
			response := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{
						Name:       "build",
						Status:     github.CheckRunCompleted,
						Conclusion: github.ConclusionSuccess,
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(response)
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
	cfg.RequiredChecks = []string{}

	ciMonitor := NewCIMonitor(ghClient, "owner", "repo", cfg)
	merger := NewAutoMerger(ghClient, nil, ciMonitor, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc123def",
	}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if !ciCheckCalled {
		t.Error("CI check should have been called for stage environment")
	}
	if !mergeWasCalled {
		t.Error("merge should have been called after CI verification passed")
	}
}

func TestAutoMerger_MergePR_StageWithCIFailure(t *testing.T) {
	// Stage environment should block merge when CI fails
	mergeWasCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc123def/check-runs":
			response := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{
						Name:       "build",
						Status:     github.CheckRunCompleted,
						Conclusion: github.ConclusionFailure,
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(response)
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
	cfg.RequiredChecks = []string{}

	ciMonitor := NewCIMonitor(ghClient, "owner", "repo", cfg)
	merger := NewAutoMerger(ghClient, nil, ciMonitor, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc123def",
	}

	err := merger.MergePR(context.Background(), prState)
	if err == nil {
		t.Error("MergePR() should fail when CI is failing")
	}

	if mergeWasCalled {
		t.Error("merge should NOT have been called when CI is failing")
	}
}

func TestAutoMerger_MergePR_DevWithCIVerification(t *testing.T) {
	// Dev environment now calls CI verification like all other environments
	ciCheckCalled := false
	mergeWasCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/commits/abc123def/check-runs":
			ciCheckCalled = true
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: github.CheckRunCompleted, Conclusion: github.ConclusionSuccess},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
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
	cfg.Environment = EnvDev
	cfg.AutoReview = false
	cfg.RequiredChecks = []string{"build"}

	ciMonitor := NewCIMonitor(ghClient, "owner", "repo", cfg)
	merger := NewAutoMerger(ghClient, nil, ciMonitor, "owner", "repo", cfg)

	prState := &PRState{
		PRNumber: 42,
		HeadSHA:  "abc123def",
	}

	err := merger.MergePR(context.Background(), prState)
	if err != nil {
		t.Errorf("MergePR() error = %v", err)
	}

	if !ciCheckCalled {
		t.Error("CI check should have been called for dev environment")
	}
	if !mergeWasCalled {
		t.Error("merge should have been called for dev environment")
	}
}
