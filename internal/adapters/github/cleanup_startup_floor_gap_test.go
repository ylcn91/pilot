package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/testutil"
)

// TestCleaner_StartupRecover_StalenessFloorBoundary pins the 30-minute staleness
// floor in StartupRecover at its boundary: a running execution row created
// ~25 minutes ago is still considered LIVE (label kept), while one created
// ~35 minutes ago is considered ORPHANED (label stripped). The existing
// startup-recover tests only exercise far-from-boundary ages (5 min and 3 h);
// this asserts the exact 30-minute cutoff behaves correctly on both sides.
func TestCleaner_StartupRecover_StalenessFloorBoundary(t *testing.T) {
	tests := []struct {
		name        string
		ageMinutes  int  // how far in the past created_at is backdated
		wantCleaned bool // whether the in-progress label should be stripped
		wantCount   int
	}{
		{
			name:        "25-minute-old running row is below the 30m floor (live, kept)",
			ageMinutes:  25,
			wantCleaned: false,
			wantCount:   0,
		},
		{
			name:        "35-minute-old running row is above the 30m floor (orphaned, cleaned)",
			ageMinutes:  35,
			wantCleaned: true,
			wantCount:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const issueNum = 2589
			store := createTestStore(t)
			defer func() { _ = store.Close() }()

			if err := store.SaveExecution(&memory.Execution{
				ID:          "exec-floor",
				TaskID:      fmt.Sprintf("GH-%d", issueNum),
				ProjectPath: "/test/project",
				Status:      "running",
			}); err != nil {
				t.Fatalf("SaveExecution: %v", err)
			}

			// Backdate created_at to straddle the 30-minute staleness floor.
			if _, err := store.DB().Exec(
				fmt.Sprintf(`UPDATE executions SET created_at = datetime('now', '-%d minutes') WHERE id = 'exec-floor'`, tt.ageMinutes),
			); err != nil {
				t.Fatalf("backdate created_at: %v", err)
			}

			issues := []*Issue{
				{
					Number:    issueNum,
					Title:     "Boundary issue",
					State:     StateOpen,
					Labels:    []Label{{Name: LabelInProgress}},
					UpdatedAt: time.Now().Add(-time.Duration(tt.ageMinutes) * time.Minute),
				},
			}

			var removeLabelCalled bool
			var mu sync.Mutex

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues" {
					_ = json.NewEncoder(w).Encode(issues)
					return
				}
				if r.Method == http.MethodDelete && r.URL.Path == fmt.Sprintf("/repos/owner/repo/issues/%d/labels/%s", issueNum, LabelInProgress) {
					removeLabelCalled = true
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
			cleaner, err := NewCleaner(client, store, "owner/repo", &StaleLabelCleanupConfig{
				Enabled: true, Interval: 30 * time.Minute, Threshold: 1 * time.Hour,
			})
			if err != nil {
				t.Fatalf("NewCleaner: %v", err)
			}

			n, err := cleaner.StartupRecover(context.Background())
			if err != nil {
				t.Fatalf("StartupRecover() error = %v", err)
			}

			mu.Lock()
			defer mu.Unlock()

			if removeLabelCalled != tt.wantCleaned {
				t.Errorf("RemoveLabel called = %v, want %v (age=%dm, floor=30m)",
					removeLabelCalled, tt.wantCleaned, tt.ageMinutes)
			}
			if n != tt.wantCount {
				t.Errorf("StartupRecover() returned %d, want %d", n, tt.wantCount)
			}
		})
	}
}
