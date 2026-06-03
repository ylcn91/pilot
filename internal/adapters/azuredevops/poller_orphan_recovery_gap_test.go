package azuredevops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// orphanRecoveryServer routes the Azure DevOps requests issued by
// recoverOrphanedWorkItems: a WIQL list (POST), a batch GET of work items, and
// then per-orphan GET + PATCH issued by RemoveWorkItemTag.
//
// It returns the configured orphan work items from the WIQL/batch calls and a
// per-id tag string from the single-item GET so that removeTag actually changes
// the value and triggers the PATCH.
type orphanRecoveryServer struct {
	mu          sync.Mutex
	orphans     []*WorkItem
	singleTags  map[int]string // tags returned by single-item GET (drives RemoveWorkItemTag)
	patchedTags map[int]string // captured tag value from the PATCH body, keyed by id
	patchCalls  int
	wiqlCalls   int
}

func newOrphanRecoveryServer(orphans []*WorkItem, singleTags map[int]string) *orphanRecoveryServer {
	return &orphanRecoveryServer{
		orphans:     orphans,
		singleTags:  singleTags,
		patchedTags: make(map[int]string),
	}
}

func (s *orphanRecoveryServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(r.URL.Path, "/_apis/wit/wiql"):
			s.mu.Lock()
			s.wiqlCalls++
			s.mu.Unlock()
			refs := make([]WIQLWorkItemRef, len(s.orphans))
			for i, wi := range s.orphans {
				refs[i] = WIQLWorkItemRef{ID: wi.ID}
			}
			_ = json.NewEncoder(w).Encode(WIQLQueryResult{WorkItems: refs})

		case strings.Contains(r.URL.Path, "/_apis/wit/workitems") && r.URL.Query().Get("ids") != "":
			// batch GET
			_ = json.NewEncoder(w).Encode(struct {
				Count int         `json:"count"`
				Value []*WorkItem `json:"value"`
			}{Count: len(s.orphans), Value: s.orphans})

		case strings.Contains(r.URL.Path, "/_apis/wit/workitems/"):
			// single work item GET or PATCH (RemoveWorkItemTag path)
			id := idFromWorkItemPath(t, r.URL.Path)
			if r.Method == http.MethodPatch {
				s.mu.Lock()
				s.patchCalls++
				var ops []PatchOperation
				_ = json.NewDecoder(r.Body).Decode(&ops)
				for _, op := range ops {
					if op.Path == "/fields/System.Tags" {
						if v, ok := op.Value.(string); ok {
							s.patchedTags[id] = v
						}
					}
				}
				s.mu.Unlock()
				_ = json.NewEncoder(w).Encode(WorkItem{ID: id})
				return
			}
			tags := s.singleTags[id]
			_ = json.NewEncoder(w).Encode(WorkItem{
				ID:     id,
				Fields: map[string]interface{}{"System.Tags": tags},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}
}

func idFromWorkItemPath(t *testing.T, path string) int {
	t.Helper()
	// path like /org/project/_apis/wit/workitems/123
	idx := strings.LastIndex(path, "/")
	if idx < 0 || idx == len(path)-1 {
		t.Fatalf("could not extract id from path %q", path)
	}
	var id int
	for _, c := range path[idx+1:] {
		if c < '0' || c > '9' {
			t.Fatalf("non-numeric id segment in path %q", path)
		}
		id = id*10 + int(c-'0')
	}
	return id
}

func TestRecoverOrphanedWorkItems_RemovesTagAndClearsProcessed(t *testing.T) {
	orphans := []*WorkItem{
		{
			ID: 11,
			Fields: map[string]interface{}{
				"System.Title": "orphan one",
				"System.Tags":  "pilot; " + TagInProgress,
			},
		},
		{
			ID: 22,
			Fields: map[string]interface{}{
				"System.Title": "orphan two",
				"System.Tags":  "pilot; " + TagInProgress,
			},
		},
	}
	// Single-item GET returns the in-progress tag so removeTag produces a diff and PATCH fires.
	singleTags := map[int]string{
		11: "pilot; " + TagInProgress,
		22: "pilot; " + TagInProgress,
	}

	srv := newOrphanRecoveryServer(orphans, singleTags)
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	poller := NewPoller(client, "pilot", 30*time.Second)

	// Pre-seed processed map so we can assert recovery clears it.
	poller.markProcessed(11)
	poller.markProcessed(22)

	poller.recoverOrphanedWorkItems(context.Background())

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.patchCalls != 2 {
		t.Errorf("expected 2 PATCH calls (one per orphan), got %d", srv.patchCalls)
	}
	for _, id := range []int{11, 22} {
		got, ok := srv.patchedTags[id]
		if !ok {
			t.Errorf("expected PATCH for orphan %d", id)
			continue
		}
		if strings.Contains(got, TagInProgress) {
			t.Errorf("orphan %d: expected in-progress tag removed, got tags %q", id, got)
		}
		if poller.IsProcessed(id) {
			t.Errorf("orphan %d: expected cleared from processed map after recovery", id)
		}
	}
}

func TestRecoverOrphanedWorkItems_NoOrphansNoop(t *testing.T) {
	srv := newOrphanRecoveryServer(nil, map[int]string{})
	server := httptest.NewServer(srv.handler(t))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	poller := NewPoller(client, "pilot", 30*time.Second)

	poller.recoverOrphanedWorkItems(context.Background())

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.wiqlCalls != 1 {
		t.Errorf("expected exactly 1 WIQL call, got %d", srv.wiqlCalls)
	}
	if srv.patchCalls != 0 {
		t.Errorf("expected no PATCH calls when there are no orphans, got %d", srv.patchCalls)
	}
}

func TestRecoverOrphanedWorkItems_ListErrorIsSwallowed(t *testing.T) {
	// WIQL endpoint returns 500; recovery should log and return without panicking.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeAzureDevOpsPAT, "org", "project", server.URL)
	poller := NewPoller(client, "pilot", 30*time.Second)

	// Should not panic and should leave processed map untouched.
	poller.markProcessed(5)
	poller.recoverOrphanedWorkItems(context.Background())

	if !poller.IsProcessed(5) {
		t.Error("expected processed map untouched when list fails")
	}
}
