package asana

import (
	"sync"
	"time"
)

// MockProcessedStore implements ProcessedStore for testing
type MockProcessedStore struct {
	processed map[string]bool
	mu        sync.RWMutex
}

func NewMockProcessedStore() *MockProcessedStore {
	return &MockProcessedStore{
		processed: make(map[string]bool),
	}
}

func (m *MockProcessedStore) Mark(source, repo, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed[issueID] = true
	return nil
}

func (m *MockProcessedStore) Unmark(source, repo, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processed, issueID)
	return nil
}

func (m *MockProcessedStore) IsProcessed(source, repo, issueID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processed[issueID], nil
}

func (m *MockProcessedStore) Load(source, repo string) (map[string]time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]time.Time)
	for k := range m.processed {
		result[k] = time.Now()
	}
	return result, nil
}
