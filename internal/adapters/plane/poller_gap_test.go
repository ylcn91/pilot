package plane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// planeFakeServer is a routing httptest server that emulates the subset of the
// Plane REST API exercised by the poller. It records label-array PATCH calls
// and state-transition PATCH calls so tests can assert on side effects.
//
// It reuses the same on-the-wire shapes (paginatedResponse, statesResponse,
// WorkItem) as the existing client_test.go helpers rather than introducing a
// new mock framework.
type planeFakeServer struct {
	mu sync.Mutex

	// items keyed by work item ID; reflects current label state.
	items map[string]*WorkItem
	// itemsByLabel is the label-filtered ListWorkItems response keyed by labelID.
	itemsByLabel map[string][]WorkItem
	// states returned by ListStates for every project.
	states []State

	// recorded side effects
	labelPatches []map[string]interface{} // each PATCH body that set "labels"
	statePatches []string                  // state UUIDs from PATCH bodies that set "state"
}

func newPlaneFakeServer() *planeFakeServer {
	return &planeFakeServer{
		items:        make(map[string]*WorkItem),
		itemsByLabel: make(map[string][]WorkItem),
	}
}

func (f *planeFakeServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != testutil.FakePlaneAPIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		path := r.URL.Path

		// ListStates: .../states/
		if strings.HasSuffix(path, "/states/") {
			f.mu.Lock()
			resp := statesResponse{Results: f.states}
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Work items collection: .../work-items/ (GET list, optional ?label=)
		if strings.HasSuffix(path, "/work-items/") && r.Method == http.MethodGet {
			labelID := r.URL.Query().Get("label")
			f.mu.Lock()
			items := f.itemsByLabel[labelID]
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(paginatedResponse{Results: items, TotalCount: len(items)})
			return
		}

		// Single work item: .../work-items/{id}/ (GET or PATCH)
		if strings.Contains(path, "/work-items/") && strings.HasSuffix(path, "/") {
			id := workItemIDFromPath(path)
			switch r.Method {
			case http.MethodGet:
				f.mu.Lock()
				item := f.items[id]
				f.mu.Unlock()
				if item == nil {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(item)
				return
			case http.MethodPatch:
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				f.mu.Lock()
				if _, ok := body["labels"]; ok {
					f.labelPatches = append(f.labelPatches, body)
					// Reflect the new label set back onto the stored item so a
					// subsequent GetWorkItem (e.g. AddLabel after RemoveLabel)
					// sees the updated array.
					if item := f.items[id]; item != nil {
						item.LabelIDs = toStringSlice(body["labels"])
					}
				}
				if s, ok := body["state"]; ok {
					if str, ok := s.(string); ok {
						f.statePatches = append(f.statePatches, str)
					}
				}
				f.mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}
}

func (f *planeFakeServer) labelPatchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.labelPatches)
}

func (f *planeFakeServer) statePatchesCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.statePatches))
	copy(out, f.statePatches)
	return out
}

// labelPatchesContain reports whether any recorded labels PATCH produced a
// label array containing labelID.
func (f *planeFakeServer) labelPatchesContain(labelID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, body := range f.labelPatches {
		for _, id := range toStringSlice(body["labels"]) {
			if id == labelID {
				return true
			}
		}
	}
	return false
}

// toStringSlice coerces a decoded JSON labels array (which arrives as
// []interface{}) into []string.
func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		// already []string?
		if s, ok := v.([]string); ok {
			return s
		}
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func workItemIDFromPath(path string) string {
	// .../work-items/{id}/
	trimmed := strings.TrimSuffix(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// newTestPoller builds a Poller wired to a Client pointing at srv, with the
// supplied config and options.
func newTestPoller(srvURL string, cfg *Config, opts ...PollerOption) *Poller {
	client := NewClient(srvURL, testutil.FakePlaneAPIKey)
	return NewPoller(client, cfg, time.Millisecond, opts...)
}

// --- Gap 1: recoverOrphanedIssues ---

func TestRecoverOrphanedIssues(t *testing.T) {
	t.Run("no-op when in-progress label not resolved", func(t *testing.T) {
		fake := newPlaneFakeServer()
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg)
		// inProgressLabelID intentionally left empty.
		p.recoverOrphanedIssues(context.Background())

		if fake.labelPatchCount() != 0 {
			t.Fatalf("expected no label PATCH when in-progress label unresolved, got %d", fake.labelPatchCount())
		}
	})

	t.Run("removes in-progress label and clears processed", func(t *testing.T) {
		fake := newPlaneFakeServer()
		// One orphaned item that still carries the in-progress label.
		fake.items["wi-orphan"] = &WorkItem{
			ID:        "wi-orphan",
			Name:      "Orphaned work",
			ProjectID: "proj-1",
			LabelIDs:  []string{"lbl-inprogress", "lbl-pilot"},
		}
		fake.itemsByLabel["lbl-inprogress"] = []WorkItem{
			{ID: "wi-orphan", Name: "Orphaned work", ProjectID: "proj-1", LabelIDs: []string{"lbl-inprogress", "lbl-pilot"}},
		}
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		store := newFakeProcessedStore()
		_ = store.Mark("plane", "", "wi-orphan")

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg, WithProcessedStore(store))
		p.inProgressLabelID = "lbl-inprogress"
		// Mark as processed in-memory to verify it gets cleared.
		p.markProcessed("wi-orphan")

		p.recoverOrphanedIssues(context.Background())

		// RemoveLabel issues exactly one labels PATCH (the in-progress label was present).
		if got := fake.labelPatchCount(); got != 1 {
			t.Fatalf("expected exactly 1 labels PATCH, got %d", got)
		}
		// The final label array must NOT contain the in-progress label.
		if fake.labelPatchesContain("lbl-inprogress") {
			t.Error("in-progress label should have been removed from the work item")
		}
		// Other labels must be preserved.
		if !fake.labelPatchesContain("lbl-pilot") {
			t.Error("non-status labels should be preserved when removing in-progress label")
		}
		// ClearProcessed must clear both the in-memory map and the store.
		if p.IsProcessed("wi-orphan") {
			t.Error("expected orphaned item to be cleared from processed map")
		}
		if isProc, _ := store.IsProcessed("plane", "", "wi-orphan"); isProc {
			t.Error("expected orphaned item to be unmarked in processed store")
		}
	})

	t.Run("skips project with no orphaned items", func(t *testing.T) {
		fake := newPlaneFakeServer()
		// itemsByLabel for the in-progress label is empty -> len(items)==0 path.
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg)
		p.inProgressLabelID = "lbl-inprogress"

		p.recoverOrphanedIssues(context.Background())

		if fake.labelPatchCount() != 0 {
			t.Fatalf("expected no label PATCH when no orphans exist, got %d", fake.labelPatchCount())
		}
	})

	t.Run("continues on ListWorkItems error", func(t *testing.T) {
		// Server returns 500 for the list call; recover should log and not panic.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-Key") != testutil.FakePlaneAPIKey {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1", "proj-2"}}
		p := newTestPoller(srv.URL, cfg)
		p.inProgressLabelID = "lbl-inprogress"

		// Must not panic and must return cleanly.
		p.recoverOrphanedIssues(context.Background())
	})
}

// --- Gap 2: cacheStateIDs + UpdateIssueState transition ---

func TestCacheStateIDs(t *testing.T) {
	t.Run("resolves started and completed state UUIDs per project", func(t *testing.T) {
		fake := newPlaneFakeServer()
		fake.states = []State{
			{ID: "s-backlog", Name: "Backlog", Group: StateGroupBacklog},
			{ID: "s-started", Name: "In Progress", Group: StateGroupStarted},
			{ID: "s-done", Name: "Done", Group: StateGroupCompleted},
		}
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg)

		p.cacheStateIDs(context.Background())

		if got := p.startedStateIDs["proj-1"]; got != "s-started" {
			t.Errorf("started state for proj-1 = %q, want s-started", got)
		}
		if got := p.completedStateIDs["proj-1"]; got != "s-done" {
			t.Errorf("completed state for proj-1 = %q, want s-done", got)
		}
	})

	t.Run("keeps first match when multiple states share a group", func(t *testing.T) {
		fake := newPlaneFakeServer()
		fake.states = []State{
			{ID: "s-started-a", Name: "In Progress", Group: StateGroupStarted},
			{ID: "s-started-b", Name: "In Review", Group: StateGroupStarted},
		}
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg)

		p.cacheStateIDs(context.Background())

		if got := p.startedStateIDs["proj-1"]; got != "s-started-a" {
			t.Errorf("started state should be first match s-started-a, got %q", got)
		}
	})

	t.Run("leaves maps empty on ListStates error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg)

		p.cacheStateIDs(context.Background())

		if len(p.startedStateIDs) != 0 {
			t.Errorf("expected no started states cached on error, got %v", p.startedStateIDs)
		}
		if len(p.completedStateIDs) != 0 {
			t.Errorf("expected no completed states cached on error, got %v", p.completedStateIDs)
		}
	})
}

// TestProcessIssueAsyncStateTransitions verifies that a successful processing
// run transitions the work item to started then completed state UUIDs.
func TestProcessIssueAsyncStateTransitions(t *testing.T) {
	fake := newPlaneFakeServer()
	fake.items["wi-1"] = &WorkItem{ID: "wi-1", Name: "Task", ProjectID: "proj-1"}
	fake.states = []State{
		{ID: "s-started", Group: StateGroupStarted},
		{ID: "s-done", Group: StateGroupCompleted},
	}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
	p := newTestPoller(srv.URL, cfg, WithOnIssue(func(_ context.Context, _ *WorkItem) (*IssueResult, error) {
		return &IssueResult{Success: true}, nil
	}))
	// Resolve state caches the way Start would.
	p.cacheStateIDs(context.Background())

	p.activeWg.Add(1)
	p.semaphore <- struct{}{}
	p.processIssueAsync(context.Background(), WorkItem{ID: "wi-1", Name: "Task", ProjectID: "proj-1"})

	states := fake.statePatchesCopy()
	// Expect started then completed.
	if len(states) != 2 {
		t.Fatalf("expected 2 state transitions, got %d: %v", len(states), states)
	}
	if states[0] != "s-started" {
		t.Errorf("first transition = %q, want s-started", states[0])
	}
	if states[1] != "s-done" {
		t.Errorf("second transition = %q, want s-done", states[1])
	}
}

// --- Gap 3: processIssueAsync error path ---

func TestProcessIssueAsyncErrorPath(t *testing.T) {
	t.Run("on handler error removes in-progress and adds failed label", func(t *testing.T) {
		fake := newPlaneFakeServer()
		// Item starts with the in-progress label already applied.
		fake.items["wi-err"] = &WorkItem{
			ID:        "wi-err",
			Name:      "Failing task",
			ProjectID: "proj-1",
			LabelIDs:  []string{"lbl-inprogress"},
		}
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg, WithOnIssue(func(_ context.Context, _ *WorkItem) (*IssueResult, error) {
			return nil, errors.New("execution blew up")
		}))
		p.inProgressLabelID = "lbl-inprogress"
		p.failedLabelID = "lbl-failed"

		p.activeWg.Add(1)
		p.semaphore <- struct{}{}
		p.processIssueAsync(context.Background(), WorkItem{
			ID:        "wi-err",
			Name:      "Failing task",
			ProjectID: "proj-1",
			LabelIDs:  []string{"lbl-inprogress"},
		})

		// The error path must add the failed label.
		if !fake.labelPatchesContain("lbl-failed") {
			t.Error("expected failed label to be added on handler error")
		}
		// The final stored label set must reflect: in-progress removed, failed added.
		fake.mu.Lock()
		finalLabels := append([]string(nil), fake.items["wi-err"].LabelIDs...)
		fake.mu.Unlock()
		if containsStr(finalLabels, "lbl-inprogress") {
			t.Errorf("in-progress label should be removed on error, final=%v", finalLabels)
		}
		if !containsStr(finalLabels, "lbl-failed") {
			t.Errorf("failed label should be present on error, final=%v", finalLabels)
		}
		// On failure, no state transition to completed should occur. Started
		// transition is skipped here because startedStateIDs is unset.
		if len(fake.statePatchesCopy()) != 0 {
			t.Errorf("expected no state transitions on error path, got %v", fake.statePatchesCopy())
		}
	})

	t.Run("nil onIssue returns immediately without API calls", func(t *testing.T) {
		fake := newPlaneFakeServer()
		srv := httptest.NewServer(fake.handler(t))
		defer srv.Close()

		cfg := &Config{WorkspaceSlug: "ws", ProjectIDs: []string{"proj-1"}}
		p := newTestPoller(srv.URL, cfg) // no WithOnIssue
		p.inProgressLabelID = "lbl-inprogress"
		p.failedLabelID = "lbl-failed"

		p.activeWg.Add(1)
		p.semaphore <- struct{}{}
		p.processIssueAsync(context.Background(), WorkItem{ID: "wi-noop", ProjectID: "proj-1"})

		if fake.labelPatchCount() != 0 {
			t.Errorf("expected no label PATCH when onIssue is nil, got %d", fake.labelPatchCount())
		}
	})
}

func containsStr(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

// fakeProcessedStore is an in-memory ProcessedStore for poller tests.
type fakeProcessedStore struct {
	mu     sync.Mutex
	marked map[string]bool
}

func newFakeProcessedStore() *fakeProcessedStore {
	return &fakeProcessedStore{marked: make(map[string]bool)}
}

func (s *fakeProcessedStore) key(source, repo, issueID string) string {
	return source + "|" + repo + "|" + issueID
}

func (s *fakeProcessedStore) Mark(source, repo, issueID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked[s.key(source, repo, issueID)] = true
	return nil
}

func (s *fakeProcessedStore) Unmark(source, repo, issueID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.marked, s.key(source, repo, issueID))
	return nil
}

func (s *fakeProcessedStore) IsProcessed(source, repo, issueID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.marked[s.key(source, repo, issueID)], nil
}

func (s *fakeProcessedStore) Load(source, repo string) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]time.Time)
	prefix := source + "|" + repo + "|"
	for k := range s.marked {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = time.Now()
		}
	}
	return out, nil
}
