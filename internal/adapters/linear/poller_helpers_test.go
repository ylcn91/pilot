package linear

import (
	"sync"
	"time"
)

// mockProcessedStore implements ProcessedStore for testing.
// GH-1351: Tests Linear processed issue persistence.
type mockProcessedStore struct {
	mu        sync.Mutex
	processed map[string]string // id -> result
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
	for id := range m.processed {
		result[id] = time.Now()
	}
	return result, nil
}
