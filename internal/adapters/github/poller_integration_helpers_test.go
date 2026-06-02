//go:build integration

package github

import (
	"strconv"
	"sync"
	"time"
)

// mockProcessedStore implements ProcessedStore for testing
type mockProcessedStore struct {
	mu        sync.Mutex
	processed map[int]bool
}

func (m *mockProcessedStore) Mark(source, repo, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, err := strconv.Atoi(issueID); err == nil {
		m.processed[id] = true
	}
	return nil
}

func (m *mockProcessedStore) Unmark(source, repo, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, err := strconv.Atoi(issueID); err == nil {
		delete(m.processed, id)
	}
	return nil
}

func (m *mockProcessedStore) IsProcessed(source, repo, issueID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, err := strconv.Atoi(issueID); err == nil {
		return m.processed[id], nil
	}
	return false, nil
}

func (m *mockProcessedStore) Load(source, repo string) (map[string]time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]time.Time)
	for k := range m.processed {
		result[strconv.Itoa(k)] = time.Now()
	}
	return result, nil
}
