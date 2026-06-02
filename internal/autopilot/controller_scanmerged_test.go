package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// GH-2251: Test that ScanRecentlyMergedPRs discovers externally-merged PRs
// and skips those already tracked.
func TestController_ScanRecentlyMergedPRs(t *testing.T) {
	recentMergedAt := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	oldMergedAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)

	tests := []struct {
		name            string
		prs             []github.PullRequest
		existingTracked []int // PR numbers already in activePRs
		wantTriggered   int
		wantPRNumbers   []int
	}{
		{
			name: "discovers externally merged pilot PR",
			prs: []github.PullRequest{
				{
					Number:         42,
					Head:           github.PRRef{Ref: "pilot/GH-100", SHA: "sha1"},
					Base:           github.PRRef{Ref: "main"},
					HTMLURL:        "https://github.com/owner/repo/pull/42",
					Title:          "feat(api): add endpoint",
					Merged:         true,
					MergedAt:       recentMergedAt,
					MergeCommitSHA: "merge-sha-42",
				},
			},
			wantTriggered: 1,
			wantPRNumbers: []int{42},
		},
		{
			name: "skips PR already tracked in activePRs",
			prs: []github.PullRequest{
				{
					Number:         42,
					Head:           github.PRRef{Ref: "pilot/GH-100", SHA: "sha1"},
					Base:           github.PRRef{Ref: "main"},
					HTMLURL:        "https://github.com/owner/repo/pull/42",
					Title:          "feat(api): add endpoint",
					Merged:         true,
					MergedAt:       recentMergedAt,
					MergeCommitSHA: "merge-sha-42",
				},
			},
			existingTracked: []int{42},
			wantTriggered:   0,
			wantPRNumbers:   []int{},
		},
		{
			name: "skips PR merged outside scan window",
			prs: []github.PullRequest{
				{
					Number:         42,
					Head:           github.PRRef{Ref: "pilot/GH-100", SHA: "sha1"},
					Base:           github.PRRef{Ref: "main"},
					HTMLURL:        "https://github.com/owner/repo/pull/42",
					Title:          "feat(api): add endpoint",
					Merged:         true,
					MergedAt:       oldMergedAt,
					MergeCommitSHA: "merge-sha-42",
				},
			},
			wantTriggered: 0,
			wantPRNumbers: []int{},
		},
		{
			name: "skips non-pilot branches and unmerged PRs",
			prs: []github.PullRequest{
				{
					Number:   1,
					Head:     github.PRRef{Ref: "feature/stuff", SHA: "sha1"},
					Base:     github.PRRef{Ref: "main"},
					HTMLURL:  "https://github.com/owner/repo/pull/1",
					Merged:   true,
					MergedAt: recentMergedAt,
				},
				{
					Number:  2,
					Head:    github.PRRef{Ref: "pilot/GH-200", SHA: "sha2"},
					Base:    github.PRRef{Ref: "main"},
					HTMLURL: "https://github.com/owner/repo/pull/2",
					Merged:  false, // closed but not merged
				},
			},
			wantTriggered: 0,
			wantPRNumbers: []int{},
		},
		{
			name: "mixed: discovers one, skips tracked and old",
			prs: []github.PullRequest{
				{
					Number:         10,
					Head:           github.PRRef{Ref: "pilot/GH-10", SHA: "sha10"},
					Base:           github.PRRef{Ref: "main"},
					HTMLURL:        "https://github.com/owner/repo/pull/10",
					Title:          "fix(db): connection leak",
					Merged:         true,
					MergedAt:       recentMergedAt,
					MergeCommitSHA: "merge-sha-10",
				},
				{
					Number:         20,
					Head:           github.PRRef{Ref: "pilot/GH-20", SHA: "sha20"},
					Base:           github.PRRef{Ref: "main"},
					HTMLURL:        "https://github.com/owner/repo/pull/20",
					Title:          "feat(ui): dashboard",
					Merged:         true,
					MergedAt:       recentMergedAt,
					MergeCommitSHA: "merge-sha-20",
				},
				{
					Number:         30,
					Head:           github.PRRef{Ref: "pilot/GH-30", SHA: "sha30"},
					Base:           github.PRRef{Ref: "main"},
					HTMLURL:        "https://github.com/owner/repo/pull/30",
					Title:          "chore: cleanup",
					Merged:         true,
					MergedAt:       oldMergedAt, // outside window
					MergeCommitSHA: "merge-sha-30",
				},
			},
			existingTracked: []int{20}, // already tracked
			wantTriggered:   1,         // only PR 10
			wantPRNumbers:   []int{10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls"):
					prs := make([]*github.PullRequest, len(tt.prs))
					for i := range tt.prs {
						prs[i] = &tt.prs[i]
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(prs)
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/releases"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("[]"))
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cfg := DefaultConfig()
			cfg.Release = &ReleaseConfig{
				Enabled:   true,
				Trigger:   "on_merge",
				TagPrefix: "v",
			}
			cfg.MergedPRScanWindow = 30 * time.Minute

			c := NewController(cfg, ghClient, nil, "owner", "repo")

			// Pre-populate tracked PRs
			for _, prNum := range tt.existingTracked {
				c.mu.Lock()
				c.activePRs[prNum] = &PRState{PRNumber: prNum, Stage: StageWaitingCI}
				c.mu.Unlock()
			}

			err := c.ScanRecentlyMergedPRs(context.Background())
			if err != nil {
				t.Fatalf("ScanRecentlyMergedPRs() error = %v", err)
			}

			// Count newly triggered PRs (exclude pre-existing tracked ones)
			triggered := 0
			for _, pr := range c.GetActivePRs() {
				isPreExisting := false
				for _, existing := range tt.existingTracked {
					if pr.PRNumber == existing {
						isPreExisting = true
						break
					}
				}
				if !isPreExisting {
					triggered++
				}
			}

			if triggered != tt.wantTriggered {
				t.Errorf("triggered %d PRs, want %d", triggered, tt.wantTriggered)
			}

			for _, wantPR := range tt.wantPRNumbers {
				found := false
				for _, pr := range c.GetActivePRs() {
					if pr.PRNumber == wantPR {
						if pr.Stage != StageReleasing {
							t.Errorf("PR %d stage = %v, want StageReleasing", wantPR, pr.Stage)
						}
						found = true
						break
					}
				}
				if !found {
					t.Errorf("PR %d not found in active PRs", wantPR)
				}
			}
		})
	}
}
