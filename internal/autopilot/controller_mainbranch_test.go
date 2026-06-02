package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestGetMainBranchSHA_RespectsResolvedEnv asserts that getMainBranchSHA reads
// the branch name from c.config.ResolvedEnv().Branch instead of the previously
// hardcoded "main" literal. TASK-291: prevents the regression where develop /
// master / trunk default repos silently failed post-merge CI monitoring.
//
// Each sub-test installs a different EnvironmentConfig and asserts that the
// GitHub branch endpoint received the matching branch name in its path.
func TestGetMainBranchSHA_RespectsResolvedEnv(t *testing.T) {
	tests := []struct {
		name           string
		envBranch      string
		wantBranchPath string // suffix of the URL path we expect GH to receive
	}{
		{name: "branch=main", envBranch: "main", wantBranchPath: "/branches/main"},
		{name: "branch=develop", envBranch: "develop", wantBranchPath: "/branches/develop"},
		{name: "branch=master", envBranch: "master", wantBranchPath: "/branches/master"},
		{name: "branch=trunk", envBranch: "trunk", wantBranchPath: "/branches/trunk"},
		{name: "empty branch falls back to main", envBranch: "", wantBranchPath: "/branches/main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestedPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/branches/") {
					requestedPath = r.URL.Path
					resp := github.Branch{
						Name:   strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/branches/"),
						Commit: github.BranchCommit{SHA: "deadbeef"},
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			// Install per-test environment directly via unexported fields (same-package access).
			// Mirrors the pattern at TestNewController_ReleaserInit.
			cfg.activeEnvName = "test-env"
			cfg.activeEnvConfig = &EnvironmentConfig{Branch: tt.envBranch}

			c := NewController(cfg, ghClient, nil, "owner", "repo")

			sha, err := c.getMainBranchSHA(context.Background())
			if err != nil {
				t.Fatalf("getMainBranchSHA error: %v", err)
			}
			if sha != "deadbeef" {
				t.Errorf("sha = %q, want deadbeef", sha)
			}
			if !strings.HasSuffix(requestedPath, tt.wantBranchPath) {
				t.Errorf("requested path %q does not end with %q", requestedPath, tt.wantBranchPath)
			}
		})
	}
}
