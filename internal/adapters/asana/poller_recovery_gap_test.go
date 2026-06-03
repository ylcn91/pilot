package asana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// readTagFromBody extracts data.tag from an Asana addTag/removeTag request body.
func readTagFromBody(t *testing.T, r *http.Request) string {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	var payload struct {
		Data struct {
			Tag string `json:"tag"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("failed to unmarshal request body %q: %v", string(raw), err)
	}
	return payload.Data.Tag
}

// GH-1355: recoverOrphanedTasks removes the in-progress tag from tasks left
// orphaned by a previous run and clears them from the processed map so the
// next poll cycle re-dispatches them.
func TestRecoverOrphanedTasks_RemovesTagAndClearsProcessed(t *testing.T) {
	var (
		mu           sync.Mutex
		removedTags  []string // taskGID:tagGID
		listedTagGID string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// In-progress tagged tasks lookup (GetActiveTasksByTag).
		if strings.HasPrefix(r.URL.Path, "/tags/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			mu.Lock()
			listedTagGID = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tags/"), "/tasks")
			mu.Unlock()
			resp := PagedResponse[Task]{
				Data: []Task{
					{GID: "orphan-1", Name: "Orphan one", Completed: false},
					{GID: "orphan-2", Name: "Orphan two", Completed: false},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// removeTag calls.
		if strings.HasSuffix(r.URL.Path, "/removeTag") {
			gid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), "/removeTag")
			tag := readTagFromBody(t, r)
			mu.Lock()
			removedTags = append(removedTags, gid+":"+tag)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	config := &Config{PilotTag: "pilot"}
	store := NewMockProcessedStore()
	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))
	poller.inProgressTagGID = "tag-ip"

	// Both orphans were marked processed by the previous run; recovery must clear them.
	poller.markProcessed("orphan-1")
	poller.markProcessed("orphan-2")

	poller.recoverOrphanedTasks(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if listedTagGID != "tag-ip" {
		t.Errorf("expected orphan lookup to query in-progress tag 'tag-ip', got %q", listedTagGID)
	}

	wantRemovals := map[string]bool{"orphan-1:tag-ip": false, "orphan-2:tag-ip": false}
	if len(removedTags) != len(wantRemovals) {
		t.Fatalf("expected %d removeTag calls, got %d (%v)", len(wantRemovals), len(removedTags), removedTags)
	}
	for _, rt := range removedTags {
		if _, ok := wantRemovals[rt]; !ok {
			t.Errorf("unexpected removeTag call: %s", rt)
		}
		wantRemovals[rt] = true
	}
	for rt, seen := range wantRemovals {
		if !seen {
			t.Errorf("expected removeTag call %s, but it did not happen", rt)
		}
	}

	// GH-2301: recovered tasks must be cleared from processed map + store so they re-dispatch.
	if poller.IsProcessed("orphan-1") {
		t.Error("expected orphan-1 to be cleared from processed map")
	}
	if poller.IsProcessed("orphan-2") {
		t.Error("expected orphan-2 to be cleared from processed map")
	}
	if proc, _ := store.IsProcessed("asana", "", "orphan-1"); proc {
		t.Error("expected orphan-1 to be cleared from processed store")
	}
}

// recoverOrphanedTasks is a no-op when no in-progress tag GID was cached
// (status tags optional), and must not call the API at all.
func TestRecoverOrphanedTasks_NoInProgressTag_NoOp(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	poller := NewPoller(client, &Config{PilotTag: "pilot"}, 30*time.Second)
	// inProgressTagGID intentionally left empty.

	poller.recoverOrphanedTasks(context.Background())

	if called {
		t.Error("expected no API calls when in-progress tag GID is unset")
	}
}

// When there are no orphaned tasks, recovery must not issue any removeTag calls.
func TestRecoverOrphanedTasks_NoOrphans_NoRemovals(t *testing.T) {
	var removeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasPrefix(r.URL.Path, "/tags/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			_ = json.NewEncoder(w).Encode(PagedResponse[Task]{Data: []Task{}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/removeTag") {
			removeCalls++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	poller := NewPoller(client, &Config{PilotTag: "pilot"}, 30*time.Second)
	poller.inProgressTagGID = "tag-ip"

	poller.recoverOrphanedTasks(context.Background())

	if removeCalls != 0 {
		t.Errorf("expected no removeTag calls when there are no orphans, got %d", removeCalls)
	}
}

// A failed lookup of in-progress tasks must be tolerated (logged + return),
// leaving the processed map untouched.
func TestRecoverOrphanedTasks_LookupError_KeepsProcessed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tags/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			http.Error(w, `{"errors":[{"message":"boom"}]}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	poller := NewPoller(client, &Config{PilotTag: "pilot"}, 30*time.Second)
	poller.inProgressTagGID = "tag-ip"
	poller.markProcessed("still-processed")

	// Must not panic and must return cleanly.
	poller.recoverOrphanedTasks(context.Background())

	if !poller.IsProcessed("still-processed") {
		t.Error("expected processed map to be untouched when orphan lookup fails")
	}
}

// When removing the in-progress tag fails for one orphan, recovery must
// continue to the next orphan and NOT clear the failed one from the
// processed map (the continue branch in recoverOrphanedTasks).
func TestRecoverOrphanedTasks_RemoveTagError_SkipsClearForFailed(t *testing.T) {
	var (
		mu          sync.Mutex
		removeCalls int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasPrefix(r.URL.Path, "/tags/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			resp := PagedResponse[Task]{
				Data: []Task{
					{GID: "fail-orphan", Name: "Fails to clear", Completed: false},
					{GID: "ok-orphan", Name: "Clears fine", Completed: false},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if strings.HasSuffix(r.URL.Path, "/removeTag") {
			mu.Lock()
			removeCalls++
			mu.Unlock()
			if strings.Contains(r.URL.Path, "fail-orphan") {
				http.Error(w, `{"errors":[{"message":"cannot remove"}]}`, http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	poller := NewPoller(client, &Config{PilotTag: "pilot"}, 30*time.Second)
	poller.inProgressTagGID = "tag-ip"
	poller.markProcessed("fail-orphan")
	poller.markProcessed("ok-orphan")

	poller.recoverOrphanedTasks(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if removeCalls != 2 {
		t.Errorf("expected removeTag attempted for both orphans (2), got %d", removeCalls)
	}

	// Failed orphan keeps its processed marker (continue before ClearProcessed).
	if !poller.IsProcessed("fail-orphan") {
		t.Error("expected fail-orphan to remain processed because tag removal failed")
	}
	// Successful orphan is cleared and will be re-dispatched.
	if poller.IsProcessed("ok-orphan") {
		t.Error("expected ok-orphan to be cleared from processed after successful tag removal")
	}
}

// GH processTaskAsync error path: when onTask returns an error, the poller must
// remove the in-progress tag and apply the failed tag, and must NOT apply the
// done tag.
func TestProcessTaskAsync_OnTaskError_AppliesFailedTag(t *testing.T) {
	var (
		mu          sync.Mutex
		addedTags   []string // taskGID:tagGID
		removedTags []string // taskGID:tagGID
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/addTag") {
			gid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), "/addTag")
			tag := readTagFromBody(t, r)
			mu.Lock()
			addedTags = append(addedTags, gid+":"+tag)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/removeTag") {
			gid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), "/removeTag")
			tag := readTagFromBody(t, r)
			mu.Lock()
			removedTags = append(removedTags, gid+":"+tag)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	poller := NewPoller(client, &Config{PilotTag: "pilot"}, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			return nil, errors.New("execution failed")
		}),
	)
	poller.inProgressTagGID = "tag-ip"
	poller.doneTagGID = "tag-done"
	poller.failedTagGID = "tag-failed"

	poller.activeWg.Add(1)
	poller.semaphore <- struct{}{}
	poller.processTaskAsync(context.Background(), &Task{GID: "task-err"})

	mu.Lock()
	defer mu.Unlock()

	// in-progress added at start, then removed on failure.
	if !contains(addedTags, "task-err:tag-ip") {
		t.Errorf("expected in-progress tag to be added, got added=%v", addedTags)
	}
	if !contains(removedTags, "task-err:tag-ip") {
		t.Errorf("expected in-progress tag to be removed on failure, got removed=%v", removedTags)
	}
	// failed tag must be applied.
	if !contains(addedTags, "task-err:tag-failed") {
		t.Errorf("expected failed tag to be applied, got added=%v", addedTags)
	}
	// done tag must NOT be applied on the error path.
	if contains(addedTags, "task-err:tag-done") {
		t.Errorf("did not expect done tag on error path, got added=%v", addedTags)
	}
}

// On the error path when no failed tag GID is cached, the poller must still
// remove the in-progress tag and must not panic / apply a done tag.
func TestProcessTaskAsync_OnTaskError_NoFailedTagConfigured(t *testing.T) {
	var (
		mu          sync.Mutex
		addedTags   []string
		removedTags []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/addTag") {
			gid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), "/addTag")
			mu.Lock()
			addedTags = append(addedTags, gid+":"+readTagFromBody(t, r))
			mu.Unlock()
		}
		if strings.HasSuffix(r.URL.Path, "/removeTag") {
			gid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), "/removeTag")
			mu.Lock()
			removedTags = append(removedTags, gid+":"+readTagFromBody(t, r))
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	poller := NewPoller(client, &Config{PilotTag: "pilot"}, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			return nil, errors.New("execution failed")
		}),
	)
	poller.inProgressTagGID = "tag-ip"
	// doneTagGID and failedTagGID intentionally empty.

	poller.activeWg.Add(1)
	poller.semaphore <- struct{}{}
	poller.processTaskAsync(context.Background(), &Task{GID: "task-err2"})

	mu.Lock()
	defer mu.Unlock()
	if !contains(removedTags, "task-err2:tag-ip") {
		t.Errorf("expected in-progress tag removed, got removed=%v", removedTags)
	}
	// Only the in-progress add should have happened (no failed/done tags configured).
	for _, a := range addedTags {
		if a != "task-err2:tag-ip" {
			t.Errorf("unexpected tag added with no done/failed GID configured: %s", a)
		}
	}
}

func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
