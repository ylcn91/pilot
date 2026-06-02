package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStore(t *testing.T) {
	// Use temp directory for test
	tmpDir, err := os.MkdirTemp("", "pilot-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify database file was created
	dbPath := filepath.Join(tmpDir, "pilot.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file not created")
	}
}
