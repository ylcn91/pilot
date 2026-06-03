package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// recoverTestServer is a minimal in-memory GitLab API fake that backs the
// recoverOrphanedIssues flow. It serves:
//   - GET  /api/v4/projects/{id}/issues          -> ListIssues (orphan lookup)
//   - GET  /api/v4/projects/{id}/issues/{iid}    -> GetIssue (used by RemoveIssueLabel)
//   - PUT  /api/v4/projects/{id}/issues/{iid}    -> label update (used by RemoveIssueLabel)
type recoverTestServer struct {
	mu sync.Mutex

	// orphans is returned verbatim from the ListIssues endpoint.
	orphans []*Issue
	// issuesByIID backs the per-issue GET used inside RemoveIssueLabel.
	issuesByIID map[int]*Issue

	// listStatus, getStatus, putStatus override the HTTP status for each
	// endpoint when non-zero. Zero means 200 OK.
	listStatus int
	getStatus  int
	putStatus  int

	// putLabels records the labels PUT for each issue IID so tests can assert
	// the in-progress label was actually removed.
	putLabels map[int][]string
	putCount  int
}

func newRecoverTestServer(orphans []*Issue) *recoverTestServer {
	byIID := make(map[int]*Issue, len(orphans))
	for _, iss := range orphans {
		byIID[iss.IID] = iss
	}
	return &recoverTestServer{
		orphans:     orphans,
		issuesByIID: byIID,
		putLabels:   make(map[int][]string),
	}
}

func (s *recoverTestServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		// ListIssues: path ends in /issues (no trailing IID).
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/issues"):
			if s.listStatus != 0 {
				w.WriteHeader(s.listStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "list error"})
				return
			}
			_ = json.NewEncoder(w).Encode(s.orphans)

		// GetIssue: GET /issues/{iid}
		case r.Method == http.MethodGet && strings.Contains(path, "/issues/"):
			if s.getStatus != 0 {
				w.WriteHeader(s.getStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "get error"})
				return
			}
			iid := iidFromPath(t, path)
			iss, ok := s.issuesByIID[iid]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "404"})
				return
			}
			_ = json.NewEncoder(w).Encode(iss)

		// Label update: PUT /issues/{iid}
		case r.Method == http.MethodPut && strings.Contains(path, "/issues/"):
			iid := iidFromPath(t, path)
			s.putCount++
			if s.putStatus != 0 {
				w.WriteHeader(s.putStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "put error"})
				return
			}
			var body struct {
				Labels []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode PUT body: %v", err)
			}
			s.putLabels[iid] = body.Labels
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "opened"})

		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func iidFromPath(t *testing.T, path string) int {
	t.Helper()
	idx := strings.LastIndex(path, "/issues/")
	if idx == -1 {
		t.Fatalf("path has no /issues/ segment: %s", path)
	}
	tail := path[idx+len("/issues/"):]
	if slash := strings.IndexByte(tail, '/'); slash != -1 {
		tail = tail[:slash]
	}
	n, err := strconv.Atoi(tail)
	if err != nil {
		t.Fatalf("could not parse IID from path %q: %v", path, err)
	}
	return n
}

// TestRecoverOrphanedIssues covers the GH-1355/GH-2301 startup recovery path:
// issues left with the in-progress label by a previous run must have that
// label removed and be cleared from the processed map so the next poll cycle
// re-picks them.
func TestRecoverOrphanedIssues(t *testing.T) {
	tests := []struct {
		name string
		// orphans returned by the ListIssues endpoint.
		orphans []*Issue
		// preProcessed seeds the poller's processed map before recovery.
		preProcessed []int
		// configure mutates the fake server before recovery runs.
		configure func(*recoverTestServer)

		wantPutCount     int
		wantStillProc    []int // IIDs expected to remain processed
		wantClearedProc  []int // IIDs expected to be cleared from processed
		wantLabelRemoved map[int]string // IID -> label that must NOT appear in the PUT
	}{
		{
			name: "single orphan recovered and cleared",
			orphans: []*Issue{
				{IID: 7, Title: "orphan-7", Labels: []string{"pilot", LabelInProgress}},
			},
			preProcessed:    []int{7},
			wantPutCount:    1,
			wantClearedProc: []int{7},
			wantLabelRemoved: map[int]string{
				7: LabelInProgress,
			},
		},
		{
			name: "multiple orphans all recovered",
			orphans: []*Issue{
				{IID: 1, Title: "orphan-1", Labels: []string{"pilot", LabelInProgress}},
				{IID: 2, Title: "orphan-2", Labels: []string{"pilot", LabelInProgress, "bug"}},
			},
			preProcessed:    []int{1, 2},
			wantPutCount:    2,
			wantClearedProc: []int{1, 2},
			wantLabelRemoved: map[int]string{
				1: LabelInProgress,
				2: LabelInProgress,
			},
		},
		{
			name:         "no orphans is a no-op",
			orphans:      []*Issue{},
			preProcessed: []int{99},
			wantPutCount: 0,
			// 99 was never an orphan; it must stay processed and untouched.
			wantStillProc: []int{99},
		},
		{
			name: "list error returns gracefully without clearing",
			orphans: []*Issue{
				{IID: 5, Title: "orphan-5", Labels: []string{"pilot", LabelInProgress}},
			},
			preProcessed: []int{5},
			configure: func(s *recoverTestServer) {
				s.listStatus = http.StatusInternalServerError
			},
			wantPutCount: 0,
			// ListIssues failed, so nothing should be cleared.
			wantStillProc: []int{5},
		},
		{
			name: "remove-label failure on one orphan does not block the next",
			orphans: []*Issue{
				{IID: 10, Title: "orphan-10", Labels: []string{"pilot", LabelInProgress}},
				{IID: 11, Title: "orphan-11", Labels: []string{"pilot", LabelInProgress}},
			},
			preProcessed: []int{10, 11},
			configure: func(s *recoverTestServer) {
				// PUT fails for every issue -> RemoveIssueLabel returns an error,
				// so the loop `continue`s and never calls ClearProcessed.
				s.putStatus = http.StatusInternalServerError
			},
			// Both issues attempt the PUT (GET succeeds, PUT fails).
			wantPutCount: 2,
			// Neither is cleared because the remove failed for both.
			wantStillProc: []int{10, 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newRecoverTestServer(tt.orphans)
			if tt.configure != nil {
				tt.configure(srv)
			}

			ts := httptest.NewServer(srv.handler(t))
			defer ts.Close()

			client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", ts.URL)
			poller := NewPoller(client, "pilot", 30*time.Second)

			for _, iid := range tt.preProcessed {
				poller.markProcessed(iid)
			}

			poller.recoverOrphanedIssues(context.Background())

			srv.mu.Lock()
			gotPutCount := srv.putCount
			putLabels := make(map[int][]string, len(srv.putLabels))
			for k, v := range srv.putLabels {
				putLabels[k] = v
			}
			srv.mu.Unlock()

			if gotPutCount != tt.wantPutCount {
				t.Errorf("PUT count = %d, want %d", gotPutCount, tt.wantPutCount)
			}

			for _, iid := range tt.wantClearedProc {
				if poller.IsProcessed(iid) {
					t.Errorf("issue %d should have been cleared from processed map", iid)
				}
			}
			for _, iid := range tt.wantStillProc {
				if !poller.IsProcessed(iid) {
					t.Errorf("issue %d should still be processed", iid)
				}
			}

			for iid, removed := range tt.wantLabelRemoved {
				labels, ok := putLabels[iid]
				if !ok {
					t.Errorf("expected a label PUT for issue %d, got none", iid)
					continue
				}
				for _, l := range labels {
					if l == removed {
						t.Errorf("issue %d PUT still contains label %q, want it removed", iid, removed)
					}
				}
			}
		})
	}
}

// TestRecoverOrphanedIssues_ClearsPersistentStore verifies the GH-2301 path
// where the orphan is also unmarked in the persistent ProcessedStore, not just
// the in-memory map, so a hot-restarted poller re-picks it.
func TestRecoverOrphanedIssues_ClearsPersistentStore(t *testing.T) {
	orphans := []*Issue{
		{IID: 21, Title: "orphan-21", Labels: []string{"pilot", LabelInProgress}},
	}
	srv := newRecoverTestServer(orphans)
	ts := httptest.NewServer(srv.handler(t))
	defer ts.Close()

	client := NewClientWithBaseURL(testutil.FakeGitLabToken, "namespace/project", ts.URL)
	store := NewMockProcessedStore()
	store.processed[21] = true

	// WithProcessedStore loads pre-seeded IDs into the in-memory map on creation.
	poller := NewPoller(client, "pilot", 30*time.Second, WithProcessedStore(store))
	if !poller.IsProcessed(21) {
		t.Fatal("issue 21 should be loaded as processed from the store")
	}

	poller.recoverOrphanedIssues(context.Background())

	if poller.IsProcessed(21) {
		t.Error("issue 21 should have been cleared from the in-memory processed map")
	}
	if store.processed[21] {
		t.Error("issue 21 should have been unmarked in the persistent store")
	}
}
