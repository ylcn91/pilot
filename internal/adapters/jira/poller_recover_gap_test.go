package jira

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

// labelMutation records a single AddLabel/RemoveLabel call captured by the
// mock server (op is "add" or "remove").
type labelMutation struct {
	op    string
	label string
}

// TestRecoverOrphanedIssues_RemovesInProgressLabelAndClearsProcessed verifies
// the GH-1355 startup recovery: issues still carrying the pilot-in-progress
// label from a previous run get that label removed and are cleared from the
// processed map so the next poll picks them up again.
func TestRecoverOrphanedIssues_RemovesInProgressLabelAndClearsProcessed(t *testing.T) {
	var (
		mu          sync.Mutex
		searchJQL   string
		mutations   = map[string][]labelMutation{}
		searchCalls int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			searchJQL, _ = body["jql"].(string)
			searchCalls++
			mu.Unlock()

			resp := SearchResponse{
				Issues: []*Issue{
					{
						Key:    "ORPH-1",
						Fields: Fields{Summary: "Orphaned one", Labels: []string{"pilot", LabelInProgress}},
					},
					{
						Key:    "ORPH-2",
						Fields: Fields{Summary: "Orphaned two", Labels: []string{"pilot", LabelInProgress}},
					},
				},
				IsLast: true,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Label add/remove use PUT /issue/{key}
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") {
			key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			update, _ := body["update"].(map[string]interface{})
			labels, _ := update["labels"].([]interface{})
			mu.Lock()
			for _, l := range labels {
				m, _ := l.(map[string]interface{})
				for op, v := range m {
					label, _ := v.(string)
					mutations[key] = append(mutations[key], labelMutation{op: op, label: label})
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
	config := &Config{PilotLabel: "pilot", ProjectKey: "ORPH"}
	poller := NewPoller(client, config, 30*time.Second)

	// Both orphans are stale in the processed map (e.g. from a prior run).
	poller.markProcessed("ORPH-1")
	poller.markProcessed("ORPH-2")

	poller.recoverOrphanedIssues(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if searchCalls != 1 {
		t.Fatalf("expected exactly 1 search call, got %d", searchCalls)
	}
	// JQL must filter by the in-progress label, project, and exclude Done.
	for _, want := range []string{
		`labels = "` + LabelInProgress + `"`,
		`project = "ORPH"`,
		"statusCategory != Done",
	} {
		if !strings.Contains(searchJQL, want) {
			t.Errorf("recovery JQL %q missing %q", searchJQL, want)
		}
	}

	for _, key := range []string{"ORPH-1", "ORPH-2"} {
		ms := mutations[key]
		if len(ms) != 1 {
			t.Fatalf("expected exactly 1 label mutation for %s, got %v", key, ms)
		}
		if ms[0].op != "remove" || ms[0].label != LabelInProgress {
			t.Errorf("%s: expected remove %q, got %+v", key, LabelInProgress, ms[0])
		}
		if poller.IsProcessed(key) {
			t.Errorf("%s should be cleared from processed map after recovery", key)
		}
	}
}

// TestRecoverOrphanedIssues_NoOrphansLeavesProcessedUntouched verifies that an
// empty search result is a no-op: no label mutations, processed map intact.
func TestRecoverOrphanedIssues_NoOrphansLeavesProcessedUntouched(t *testing.T) {
	var (
		mu          sync.Mutex
		putCalls    int
		searchCalls int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
			mu.Lock()
			searchCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(SearchResponse{Issues: []*Issue{}, IsLast: true})
			return
		}
		if r.Method == http.MethodPut {
			mu.Lock()
			putCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	poller.markProcessed("KEEP-1")

	poller.recoverOrphanedIssues(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if searchCalls != 1 {
		t.Errorf("expected 1 search call, got %d", searchCalls)
	}
	if putCalls != 0 {
		t.Errorf("expected no label mutations when no orphans, got %d PUTs", putCalls)
	}
	if !poller.IsProcessed("KEEP-1") {
		t.Error("KEEP-1 should remain processed when there are no orphans")
	}
}

// TestRecoverOrphanedIssues_SearchErrorIsTolerated verifies that a failed
// search does not panic and does not mutate the processed map (the recovery
// simply logs a warning and returns).
func TestRecoverOrphanedIssues_SearchErrorIsTolerated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any request fails with a server error.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorMessages":["boom"]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	poller.markProcessed("STAY-1")

	// Must not panic.
	poller.recoverOrphanedIssues(context.Background())

	if !poller.IsProcessed("STAY-1") {
		t.Error("STAY-1 should remain processed when the recovery search fails")
	}
}

// TestRecoverOrphanedIssues_RemoveLabelFailureSkipsClear verifies that when
// RemoveLabel fails for an orphan, the recovery does NOT clear it from the
// processed map (it continues to the next issue without re-queuing this one).
func TestRecoverOrphanedIssues_RemoveLabelFailureSkipsClear(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/search/jql" {
			resp := SearchResponse{
				Issues: []*Issue{
					{Key: "ORPH-9", Fields: Fields{Labels: []string{"pilot", LabelInProgress}}},
				},
				IsLast: true,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// RemoveLabel (PUT) fails.
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}
	poller := NewPoller(client, config, 30*time.Second)

	poller.markProcessed("ORPH-9")

	poller.recoverOrphanedIssues(context.Background())

	if !poller.IsProcessed("ORPH-9") {
		t.Error("ORPH-9 should remain processed because label removal failed (continue path)")
	}
}
