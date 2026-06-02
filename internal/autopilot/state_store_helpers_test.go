package autopilot

import (
	"testing"
)

func newTestStateStore(t *testing.T) *StateStore {
	t.Helper()
	store, err := NewStateStoreFromPath(":memory:")
	if err != nil {
		t.Fatalf("failed to create test state store: %v", err)
	}
	return store
}
