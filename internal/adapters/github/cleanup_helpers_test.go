package github

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

// Helper function to create a test memory store
func createTestStore(t *testing.T) *memory.Store {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "cleanup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Cleanup on test completion
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	store, err := memory.NewStore(filepath.Join(tmpDir, "test-db"))
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	return store
}
