package jira

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

// TestProcessIssueAsync_OnIssueError_AppliesFailedLabel verifies the error path
// of processIssueAsync: when the onIssue callback returns an error, the
// in-progress label is removed and the pilot-failed label is added (and no
// done label is applied).
func TestProcessIssueAsync_OnIssueError_AppliesFailedLabel(t *testing.T) {
	var (
		mu        sync.Mutex
		mutations []labelMutation
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			update, _ := body["update"].(map[string]interface{})
			labels, _ := update["labels"].([]interface{})
			mu.Lock()
			for _, l := range labels {
				m, _ := l.(map[string]interface{})
				for op, v := range m {
					label, _ := v.(string)
					mutations = append(mutations, labelMutation{op: op, label: label})
				}
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	var called bool
	poller := NewPoller(client, config, 30*time.Second,
		WithOnJiraIssue(func(ctx context.Context, issue *Issue) (*IssueResult, error) {
			called = true
			return nil, errors.New("execution blew up")
		}),
	)

	// processIssueAsync expects a semaphore slot held and activeWg incremented
	// by its caller; replicate that contract here.
	poller.semaphore <- struct{}{}
	poller.activeWg.Add(1)

	issue := &Issue{Key: "FAIL-1", Fields: Fields{Summary: "Will fail"}}
	poller.processIssueAsync(context.Background(), issue)

	// processIssueAsync releases the slot and Done()s the waitgroup itself.
	poller.activeWg.Wait()

	if !called {
		t.Fatal("expected onIssue callback to be invoked")
	}

	mu.Lock()
	defer mu.Unlock()

	// Expected sequence: add in-progress, remove in-progress, add failed.
	wantOps := []labelMutation{
		{op: "add", label: LabelInProgress},
		{op: "remove", label: LabelInProgress},
		{op: "add", label: LabelFailed},
	}
	if len(mutations) != len(wantOps) {
		t.Fatalf("expected %d label mutations, got %d: %+v", len(wantOps), len(mutations), mutations)
	}
	for i, want := range wantOps {
		if mutations[i] != want {
			t.Errorf("mutation[%d] = %+v, want %+v", i, mutations[i], want)
		}
	}

	// Crucially, the done label must NOT have been applied on the error path.
	for _, m := range mutations {
		if m.op == "add" && m.label == LabelDone {
			t.Error("done label must not be applied when onIssue returns an error")
		}
	}

	// Semaphore slot must have been released.
	if len(poller.semaphore) != 0 {
		t.Errorf("expected semaphore released, len=%d", len(poller.semaphore))
	}
}

// TestProcessIssueAsync_NilOnIssue_NoLabelMutations verifies the guard clause:
// with no onIssue callback configured, processIssueAsync returns immediately
// without touching any labels (and still releases its semaphore slot).
func TestProcessIssueAsync_NilOnIssue_NoLabelMutations(t *testing.T) {
	var putCalls int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			putCalls++
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second) // no WithOnJiraIssue

	poller.semaphore <- struct{}{}
	poller.activeWg.Add(1)

	poller.processIssueAsync(context.Background(), &Issue{Key: "NOOP-1"})
	poller.activeWg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if putCalls != 0 {
		t.Errorf("expected no label mutations when onIssue is nil, got %d PUTs", putCalls)
	}
	if len(poller.semaphore) != 0 {
		t.Errorf("expected semaphore released even on nil-onIssue early return, len=%d", len(poller.semaphore))
	}
}
