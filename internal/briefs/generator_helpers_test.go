package briefs

import (
	"os"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func setupTestStore(t *testing.T) (*memory.Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "briefs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	store, err := memory.NewStore(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func timePtr(t time.Time) *time.Time {
	return &t
}
