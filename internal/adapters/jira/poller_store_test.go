package jira

import (
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// GH-1357: Tests for parallel execution pattern and ProcessedStore

// mockProcessedStore implements ProcessedStore for testing.
type mockProcessedStore struct {
	mu        sync.Mutex
	processed map[string]string // key -> result
}

func newMockProcessedStore() *mockProcessedStore {
	return &mockProcessedStore{
		processed: make(map[string]string),
	}
}

func (m *mockProcessedStore) Mark(source, repo, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed[issueID] = "processed"
	return nil
}

func (m *mockProcessedStore) Unmark(source, repo, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processed, issueID)
	return nil
}

func (m *mockProcessedStore) IsProcessed(source, repo, issueID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.processed[issueID]
	return ok, nil
}

func (m *mockProcessedStore) Load(source, repo string) (map[string]time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]time.Time)
	for key := range m.processed {
		result[key] = time.Now()
	}
	return result, nil
}

// TestPoller_LoadsProcessedFromStore verifies that the poller loads
// processed issues from the store on startup (GH-1357).
func TestPoller_LoadsProcessedFromStore(t *testing.T) {
	store := newMockProcessedStore()

	// Pre-populate store with processed issues
	store.processed["TEST-1"] = "processed"
	store.processed["TEST-2"] = "processed"

	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))

	// Verify processed issues were loaded
	if poller.ProcessedCount() != 2 {
		t.Errorf("ProcessedCount = %d, want 2", poller.ProcessedCount())
	}

	if !poller.IsProcessed("TEST-1") {
		t.Error("TEST-1 should be processed")
	}
	if !poller.IsProcessed("TEST-2") {
		t.Error("TEST-2 should be processed")
	}
	if poller.IsProcessed("TEST-3") {
		t.Error("TEST-3 should not be processed")
	}
}

// TestPoller_MarkProcessed_PersistsToStore verifies that marking an issue
// as processed persists it to the store (GH-1357).
func TestPoller_MarkProcessed_PersistsToStore(t *testing.T) {
	store := newMockProcessedStore()
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))

	// Mark an issue as processed
	poller.markProcessed("TEST-NEW")

	// Verify it's in memory
	if !poller.IsProcessed("TEST-NEW") {
		t.Error("TEST-NEW should be processed in memory")
	}

	// Verify it's persisted to store
	store.mu.Lock()
	_, exists := store.processed["TEST-NEW"]
	store.mu.Unlock()
	if !exists {
		t.Error("TEST-NEW should be persisted to store")
	}
}

// TestPoller_ClearProcessed_RemovesFromStore verifies that clearing a processed
// issue removes it from both memory and store (GH-1357).
func TestPoller_ClearProcessed_RemovesFromStore(t *testing.T) {
	store := newMockProcessedStore()
	store.processed["TEST-CLEAR"] = "processed"

	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))

	// Verify issue is loaded
	if !poller.IsProcessed("TEST-CLEAR") {
		t.Error("TEST-CLEAR should be processed initially")
	}

	// Clear the issue
	poller.ClearProcessed("TEST-CLEAR")

	// Verify it's removed from memory
	if poller.IsProcessed("TEST-CLEAR") {
		t.Error("TEST-CLEAR should not be processed after clearing")
	}

	// Verify it's removed from store
	store.mu.Lock()
	_, exists := store.processed["TEST-CLEAR"]
	store.mu.Unlock()
	if exists {
		t.Error("TEST-CLEAR should be removed from store")
	}
}

// TestPoller_WithMaxConcurrent verifies that the max concurrent option is set.
func TestPoller_WithMaxConcurrent(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second, WithMaxConcurrent(5))

	if poller.maxConcurrent != 5 {
		t.Errorf("maxConcurrent = %d, want 5", poller.maxConcurrent)
	}

	if cap(poller.semaphore) != 5 {
		t.Errorf("semaphore capacity = %d, want 5", cap(poller.semaphore))
	}
}

// TestPoller_WithMaxConcurrent_DefaultsToTwo verifies that max concurrent defaults to 2.
func TestPoller_WithMaxConcurrent_DefaultsToTwo(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second)

	if poller.maxConcurrent != 2 {
		t.Errorf("default maxConcurrent = %d, want 2", poller.maxConcurrent)
	}
}

// TestPoller_WithMaxConcurrent_MinimumOne verifies that max concurrent cannot go below 1.
func TestPoller_WithMaxConcurrent_MinimumOne(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second, WithMaxConcurrent(0))

	if poller.maxConcurrent != 1 {
		t.Errorf("maxConcurrent with 0 = %d, want 1 (minimum)", poller.maxConcurrent)
	}

	poller2 := NewPoller(client, config, 30*time.Second, WithMaxConcurrent(-5))

	if poller2.maxConcurrent != 1 {
		t.Errorf("maxConcurrent with -5 = %d, want 1 (minimum)", poller2.maxConcurrent)
	}
}

// TestPoller_Drain verifies that Drain stops accepting new issues and waits for active.
func TestPoller_Drain(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second)

	// Simulate an active task
	poller.semaphore <- struct{}{}
	poller.activeWg.Add(1)

	drainDone := make(chan struct{})
	go func() {
		poller.Drain()
		close(drainDone)
	}()

	// Give Drain time to set stopping flag
	time.Sleep(10 * time.Millisecond)

	if !poller.stopping.Load() {
		t.Error("stopping should be true after Drain called")
	}

	// Complete the active task
	<-poller.semaphore
	poller.activeWg.Done()

	// Drain should complete
	select {
	case <-drainDone:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("Drain should complete after active tasks finish")
	}
}

// TestPoller_WaitForActive verifies that WaitForActive waits for goroutines.
func TestPoller_WaitForActive(t *testing.T) {
	client := NewClient("https://example.atlassian.net", testutil.FakeJiraUsername, testutil.FakeJiraAPIToken, PlatformCloud)
	config := &Config{PilotLabel: "pilot"}

	poller := NewPoller(client, config, 30*time.Second)

	// Simulate an active task
	poller.semaphore <- struct{}{}
	poller.activeWg.Add(1)

	waitDone := make(chan struct{})
	go func() {
		poller.WaitForActive()
		close(waitDone)
	}()

	// Give WaitForActive time to set stopping flag
	time.Sleep(10 * time.Millisecond)

	if !poller.stopping.Load() {
		t.Error("stopping should be true after WaitForActive called")
	}

	// Complete the active task
	<-poller.semaphore
	poller.activeWg.Done()

	// WaitForActive should complete
	select {
	case <-waitDone:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("WaitForActive should complete after active tasks finish")
	}
}
