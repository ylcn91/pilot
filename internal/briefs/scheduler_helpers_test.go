package briefs

import (
	"os"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

func setupSchedulerTestStore(t *testing.T) (*memory.Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "scheduler_test")
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
