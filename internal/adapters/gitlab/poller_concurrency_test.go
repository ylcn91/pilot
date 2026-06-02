package gitlab

import (
	"strconv"
	"testing"
	"time"
)

// GH-1358: Tests for parallel execution pattern

func TestNewPollerWithMaxConcurrent(t *testing.T) {
	client := NewClient("test-token", "namespace/project")

	// Test default maxConcurrent
	poller := NewPoller(client, "pilot", 30*time.Second)
	if poller.maxConcurrent != 2 {
		t.Errorf("default maxConcurrent = %d, want 2", poller.maxConcurrent)
	}

	// Test custom maxConcurrent
	poller = NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(5))
	if poller.maxConcurrent != 5 {
		t.Errorf("custom maxConcurrent = %d, want 5", poller.maxConcurrent)
	}

	// Test semaphore is created with correct capacity
	if cap(poller.semaphore) != 5 {
		t.Errorf("semaphore capacity = %d, want 5", cap(poller.semaphore))
	}

	// Test minimum maxConcurrent enforcement - WithMaxConcurrent enforces minimum of 1
	poller = NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(0))
	if poller.maxConcurrent != 1 {
		t.Errorf("zero maxConcurrent should become 1, got %d", poller.maxConcurrent)
	}

	poller = NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(-1))
	if poller.maxConcurrent != 1 {
		t.Errorf("negative maxConcurrent should become 1, got %d", poller.maxConcurrent)
	}
}

func TestPoller_ClearProcessed(t *testing.T) {
	client := NewClient("test-token", "namespace/project")
	poller := NewPoller(client, "pilot", 30*time.Second)

	// Mark an issue as processed
	poller.markProcessed(42)
	if !poller.IsProcessed(42) {
		t.Error("expected issue 42 to be processed after marking")
	}

	// Clear the processed flag
	poller.ClearProcessed(42)
	if poller.IsProcessed(42) {
		t.Error("expected issue 42 to not be processed after clearing")
	}

	// Clearing a non-existent ID should not panic
	poller.ClearProcessed(999)
}

func TestPoller_DrainAndWaitForActive(t *testing.T) {
	client := NewClient("test-token", "namespace/project")
	poller := NewPoller(client, "pilot", 30*time.Second, WithMaxConcurrent(2))

	// Test that WaitForActive sets stopping flag
	poller.WaitForActive()
	if !poller.stopping.Load() {
		t.Error("expected stopping flag to be true after WaitForActive")
	}

	// Reset for next test
	poller.stopping.Store(false)

	// Test that Drain sets stopping flag
	poller.Drain()
	if !poller.stopping.Load() {
		t.Error("expected stopping flag to be true after Drain")
	}
}

func TestPoller_hasStatusLabel(t *testing.T) {
	client := NewClient("test-token", "namespace/project")
	poller := NewPoller(client, "pilot", 30*time.Second)

	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{
			name:   "no status labels",
			labels: []string{"pilot", "bug"},
			want:   false,
		},
		{
			name:   "in-progress label",
			labels: []string{"pilot", LabelInProgress},
			want:   true,
		},
		{
			name:   "done label",
			labels: []string{"pilot", LabelDone},
			want:   true,
		},
		{
			name:   "failed label",
			labels: []string{"pilot", LabelFailed},
			want:   true,
		},
		{
			name:   "empty labels",
			labels: []string{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{
				IID:    1,
				Labels: tt.labels,
			}
			got := poller.hasStatusLabel(issue)
			if got != tt.want {
				t.Errorf("hasStatusLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
	client := NewClient("test-token", "namespace/project")
	store := NewMockProcessedStore()

	// Pre-populate store
	store.processed[100] = true
	store.processed[200] = true

	poller := NewPoller(client, "pilot", 30*time.Second, WithProcessedStore(store))

	// Verify processed issues were loaded from store
	if poller.ProcessedCount() != 2 {
		t.Errorf("expected 2 processed issues loaded, got %d", poller.ProcessedCount())
	}

	if !poller.IsProcessed(100) {
		t.Error("expected issue 100 to be processed (loaded from store)")
	}

	if !poller.IsProcessed(200) {
		t.Error("expected issue 200 to be processed (loaded from store)")
	}

	// Mark a new issue as processed
	poller.markProcessed(300)

	// Verify it was persisted to store
	if !store.processed[300] {
		t.Error("expected issue 300 to be persisted to store")
	}

	// Clear a processed issue
	poller.ClearProcessed(100)

	// Verify it was removed from store
	if store.processed[100] {
		t.Error("expected issue 100 to be removed from store")
	}
}
