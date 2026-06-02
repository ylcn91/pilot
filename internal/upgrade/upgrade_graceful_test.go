package upgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGracefulUpgrader_PerformUpgrade(t *testing.T) {
	// Create a mock HTTP server serving a valid release asset
	binaryContent := []byte("new-binary-content")
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(binaryContent)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(binaryContent)
	}))
	defer assetServer.Close()

	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
		t.Fatalf("Failed to create test binary: %v", err)
	}

	statePath := filepath.Join(tempDir, "upgrade-state.json")

	upgrader := &Upgrader{
		currentVersion: "0.1.0",
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		binaryPath:     binaryPath,
		backupPath:     binaryPath + BackupSuffix,
	}

	t.Run("success with no running tasks", func(t *testing.T) {
		g := &GracefulUpgrader{
			upgrader:    upgrader,
			statePath:   statePath,
			taskChecker: &NoOpTaskChecker{},
		}

		release := &Release{
			TagName: "v0.2.0",
			Assets: []Asset{
				{
					Name:               fmt.Sprintf("pilot-%s-%s", runtime.GOOS, runtime.GOARCH),
					BrowserDownloadURL: assetServer.URL,
					Size:               int64(len(binaryContent)),
				},
			},
		}

		var progressCalled bool
		opts := &UpgradeOptions{
			WaitForTasks: false,
			Force:        true,
			OnProgress: func(pct int, msg string) {
				progressCalled = true
			},
		}

		if err := g.PerformUpgrade(context.Background(), release, opts); err != nil {
			t.Fatalf("PerformUpgrade() error = %v", err)
		}

		if !progressCalled {
			t.Error("OnProgress callback was not called")
		}

		// State file should show completed
		state, err := LoadState(statePath)
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}
		if state.Status != StatusCompleted {
			t.Errorf("State.Status = %q, want %q", state.Status, StatusCompleted)
		}
	})

	t.Run("nil opts uses defaults", func(t *testing.T) {
		// Restore binary for next test
		if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
			t.Fatalf("Failed to restore binary: %v", err)
		}

		g := &GracefulUpgrader{
			upgrader:    upgrader,
			statePath:   statePath,
			taskChecker: &NoOpTaskChecker{},
		}

		release := &Release{
			TagName: "v0.2.0",
			Assets: []Asset{
				{
					Name:               fmt.Sprintf("pilot-%s-%s", runtime.GOOS, runtime.GOARCH),
					BrowserDownloadURL: assetServer.URL,
					Size:               int64(len(binaryContent)),
				},
			},
		}

		if err := g.PerformUpgrade(context.Background(), release, nil); err != nil {
			t.Fatalf("PerformUpgrade(nil opts) error = %v", err)
		}
	})

	t.Run("wait for tasks timeout", func(t *testing.T) {
		if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
			t.Fatalf("Failed to restore binary: %v", err)
		}

		checker := &mockTaskChecker{
			tasks:   []string{"task-1"},
			waitErr: context.DeadlineExceeded,
		}

		g := &GracefulUpgrader{
			upgrader:    upgrader,
			statePath:   statePath,
			taskChecker: checker,
		}

		release := &Release{TagName: "v0.2.0"}
		opts := &UpgradeOptions{
			WaitForTasks: true,
			TaskTimeout:  time.Millisecond,
			Force:        false,
		}

		err := g.PerformUpgrade(context.Background(), release, opts)
		if err == nil {
			t.Fatal("PerformUpgrade() should return error on task timeout")
		}
	})
}

func TestGracefulUpgrader_CheckAndRollback(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	backupPath := binaryPath + BackupSuffix
	statePath := filepath.Join(tempDir, "upgrade-state.json")

	t.Run("no state file", func(t *testing.T) {
		g := &GracefulUpgrader{
			upgrader: &Upgrader{
				binaryPath: binaryPath,
				backupPath: backupPath,
			},
			statePath: statePath,
		}

		rolled, err := g.CheckAndRollback()
		if err != nil {
			t.Fatalf("CheckAndRollback() error = %v", err)
		}
		if rolled {
			t.Error("CheckAndRollback() = true, want false when no state file")
		}
	})

	t.Run("rollback on failed state", func(t *testing.T) {
		// Create binary and backup
		if err := os.WriteFile(binaryPath, []byte("new-bad-binary"), 0755); err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}
		if err := os.WriteFile(backupPath, []byte("original-good-binary"), 0755); err != nil {
			t.Fatalf("Failed to create backup: %v", err)
		}

		// Create failed state
		state := &State{
			Status:     StatusFailed,
			BackupPath: backupPath,
			Error:      "upgrade failed",
		}
		if err := state.Save(statePath); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		g := &GracefulUpgrader{
			upgrader: &Upgrader{
				binaryPath: binaryPath,
				backupPath: backupPath,
			},
			statePath: statePath,
		}

		rolled, err := g.CheckAndRollback()
		if err != nil {
			t.Fatalf("CheckAndRollback() error = %v", err)
		}
		if !rolled {
			t.Error("CheckAndRollback() = false, want true for failed state")
		}

		// Verify binary was restored
		content, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("Failed to read binary: %v", err)
		}
		if string(content) != "original-good-binary" {
			t.Errorf("Binary content = %q, want %q", string(content), "original-good-binary")
		}
	})

	t.Run("no rollback for completed state", func(t *testing.T) {
		state := &State{
			Status: StatusCompleted,
		}
		if err := state.Save(statePath); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		g := &GracefulUpgrader{
			upgrader: &Upgrader{
				binaryPath: binaryPath,
				backupPath: backupPath,
			},
			statePath: statePath,
		}

		rolled, err := g.CheckAndRollback()
		if err != nil {
			t.Fatalf("CheckAndRollback() error = %v", err)
		}
		if rolled {
			t.Error("CheckAndRollback() = true, want false for completed state")
		}
	})
}

func TestGracefulUpgrader_CleanupState(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	backupPath := binaryPath + BackupSuffix
	statePath := filepath.Join(tempDir, "upgrade-state.json")

	t.Run("cleanup completed state", func(t *testing.T) {
		// Create backup file
		if err := os.WriteFile(backupPath, []byte("backup"), 0755); err != nil {
			t.Fatalf("Failed to create backup: %v", err)
		}

		state := &State{Status: StatusCompleted}
		if err := state.Save(statePath); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		g := &GracefulUpgrader{
			upgrader: &Upgrader{
				binaryPath: binaryPath,
				backupPath: backupPath,
			},
			statePath: statePath,
		}

		if err := g.CleanupState(); err != nil {
			t.Fatalf("CleanupState() error = %v", err)
		}

		// State should be cleared
		loaded, err := LoadState(statePath)
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}
		if loaded != nil {
			t.Error("State should be nil after cleanup")
		}
	})

	t.Run("no cleanup for non-completed state", func(t *testing.T) {
		state := &State{Status: StatusFailed}
		if err := state.Save(statePath); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		g := &GracefulUpgrader{
			upgrader: &Upgrader{
				binaryPath: binaryPath,
				backupPath: backupPath,
			},
			statePath: statePath,
		}

		if err := g.CleanupState(); err != nil {
			t.Fatalf("CleanupState() error = %v", err)
		}

		// State should still exist
		loaded, err := LoadState(statePath)
		if err != nil {
			t.Fatalf("LoadState() error = %v", err)
		}
		if loaded == nil {
			t.Error("State should not be cleared for non-completed status")
		}
	})

	t.Run("no state file is ok", func(t *testing.T) {
		_ = os.Remove(statePath)

		g := &GracefulUpgrader{
			upgrader: &Upgrader{
				binaryPath: binaryPath,
				backupPath: backupPath,
			},
			statePath: statePath,
		}

		if err := g.CleanupState(); err != nil {
			t.Fatalf("CleanupState() error = %v, should handle missing state", err)
		}
	})
}
