package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// ciFailureServer returns an httptest server whose check-runs endpoint reports a
// failing build for SHA abc1234 and accepts issue creation + PR close.
func ciFailureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/abc1234/check-runs":
			resp := github.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []github.CheckRun{
					{Name: "build", Status: "completed", Conclusion: "failure"},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/repos/owner/repo/issues" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(github.Issue{Number: 200})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

// #2: notify_on_failure must gate the CI failure notification.
func TestController_NotifyOnFailure_Gate(t *testing.T) {
	tests := []struct {
		name            string
		notifyOnFailure bool
		wantNotified    bool
	}{
		{name: "enabled fires notification", notifyOnFailure: true, wantNotified: true},
		{name: "disabled suppresses notification", notifyOnFailure: false, wantNotified: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := ciFailureServer(t)
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.Environment = EnvStage
			cfg.CIPollInterval = 10 * time.Millisecond
			cfg.CIWaitTimeout = 1 * time.Second
			cfg.AutoCreateIssues = true
			cfg.NotifyOnFailure = tt.notifyOnFailure

			notified := false
			notifier := &mockNotifier{
				notifyCIFailedFunc: func(context.Context, *PRState, []string) error {
					notified = true
					return nil
				},
			}

			c := NewController(cfg, ghClient, nil, "owner", "repo")
			c.SetNotifier(notifier)
			// IssueNumber 0 so handleCIFailed skips the iteration-meta fetch.
			c.OnPRCreated(42, "https://github.com/owner/repo/pull/42", 0, "abc1234", "pilot/GH-42", "")

			ctx := context.Background()
			// PR created -> waiting CI
			if err := c.ProcessPR(ctx, 42, nil); err != nil {
				t.Fatalf("ProcessPR stage 1 error: %v", err)
			}
			// waiting CI -> CI failed
			if err := c.ProcessPR(ctx, 42, nil); err != nil {
				t.Fatalf("ProcessPR stage 2 error: %v", err)
			}
			pr, _ := c.GetPRState(42)
			if pr.Stage != StageCIFailed {
				t.Fatalf("after stage 2: Stage = %s, want %s", pr.Stage, StageCIFailed)
			}
			// CI failed -> fix issue + (maybe) notification
			if err := c.ProcessPR(ctx, 42, nil); err != nil {
				t.Fatalf("ProcessPR stage 3 error: %v", err)
			}

			if notified != tt.wantNotified {
				t.Errorf("NotifyCIFailed called = %v, want %v", notified, tt.wantNotified)
			}
		})
	}
}
