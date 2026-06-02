package azuredevops

import (
	"strconv"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// MockProcessedStore implements ProcessedStore for testing
type MockProcessedStore struct {
	processed map[int]bool
}

func NewMockProcessedStore() *MockProcessedStore {
	return &MockProcessedStore{
		processed: make(map[int]bool),
	}
}

func (m *MockProcessedStore) Mark(source, repo, issueID string) error {
	if id, err := strconv.Atoi(issueID); err == nil {
		m.processed[id] = true
	}
	return nil
}

func (m *MockProcessedStore) Unmark(source, repo, issueID string) error {
	if id, err := strconv.Atoi(issueID); err == nil {
		delete(m.processed, id)
	}
	return nil
}

func (m *MockProcessedStore) IsProcessed(source, repo, issueID string) (bool, error) {
	if id, err := strconv.Atoi(issueID); err == nil {
		return m.processed[id], nil
	}
	return false, nil
}

func (m *MockProcessedStore) Load(source, repo string) (map[string]time.Time, error) {
	result := make(map[string]time.Time)
	for k := range m.processed {
		result[strconv.Itoa(k)] = time.Now()
	}
	return result, nil
}

func TestPollerWithProcessedStore(t *testing.T) {
	client := NewClient(testutil.FakeAzureDevOpsPAT, "org", "project")
	store := NewMockProcessedStore()

	// Pre-populate store
	store.processed[100] = true
	store.processed[200] = true

	poller := NewPoller(client, "pilot", 30*time.Second, WithProcessedStore(store))

	// Verify processed work items were loaded from store
	if poller.ProcessedCount() != 2 {
		t.Errorf("expected 2 processed work items loaded, got %d", poller.ProcessedCount())
	}

	if !poller.IsProcessed(100) {
		t.Error("expected work item 100 to be processed (loaded from store)")
	}

	if !poller.IsProcessed(200) {
		t.Error("expected work item 200 to be processed (loaded from store)")
	}

	// Mark a new work item as processed
	poller.markProcessed(300)

	// Verify it was persisted to store
	if !store.processed[300] {
		t.Error("expected work item 300 to be persisted to store")
	}

	// Clear a processed work item
	poller.ClearProcessed(100)

	// Verify it was removed from store
	if store.processed[100] {
		t.Error("expected work item 100 to be removed from store")
	}
}
