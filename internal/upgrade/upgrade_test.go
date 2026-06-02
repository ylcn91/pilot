package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// HotUpgrader tests
// ---------------------------------------------------------------------------

func TestHotUpgrader_Accessors(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0755); err != nil {
		t.Fatalf("Failed to create binary: %v", err)
	}

	u := &Upgrader{
		currentVersion: "1.0.0",
		binaryPath:     binaryPath,
		backupPath:     binaryPath + BackupSuffix,
	}
	g := &GracefulUpgrader{upgrader: u, taskChecker: &NoOpTaskChecker{}}
	h := &HotUpgrader{graceful: g, taskChecker: &NoOpTaskChecker{}}

	if got := h.GetUpgrader(); got != u {
		t.Error("GetUpgrader() did not return expected upgrader")
	}
	if got := h.GetGracefulUpgrader(); got != g {
		t.Error("GetGracefulUpgrader() did not return expected graceful upgrader")
	}
}

func TestHotUpgrader_PerformHotUpgrade(t *testing.T) {
	binaryContent := []byte("new-hot-binary")
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(binaryContent)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(binaryContent)
	}))
	defer assetServer.Close()

	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	statePath := filepath.Join(tempDir, "upgrade-state.json")

	t.Run("task wait timeout", func(t *testing.T) {
		if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		checker := &mockTaskChecker{
			tasks:   []string{"running-task"},
			waitErr: context.DeadlineExceeded,
		}

		u := &Upgrader{
			currentVersion: "0.1.0",
			httpClient:     &http.Client{Timeout: 5 * time.Second},
			binaryPath:     binaryPath,
			backupPath:     binaryPath + BackupSuffix,
		}
		g := &GracefulUpgrader{upgrader: u, statePath: statePath, taskChecker: checker}
		h := &HotUpgrader{graceful: g, taskChecker: checker}

		release := &Release{TagName: "v0.2.0"}
		cfg := &HotUpgradeConfig{
			WaitForTasks: true,
			TaskTimeout:  time.Millisecond,
		}

		err := h.PerformHotUpgrade(context.Background(), release, cfg)
		if err == nil {
			t.Fatal("PerformHotUpgrade() should return error on task timeout")
		}
	})

	t.Run("flush session callback", func(t *testing.T) {
		if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		var flushed int32
		u := &Upgrader{
			currentVersion: "0.1.0",
			httpClient:     &http.Client{Timeout: 5 * time.Second},
			binaryPath:     binaryPath,
			backupPath:     binaryPath + BackupSuffix,
		}
		g := &GracefulUpgrader{upgrader: u, statePath: statePath, taskChecker: &NoOpTaskChecker{}}
		h := &HotUpgrader{graceful: g, taskChecker: &NoOpTaskChecker{}}

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

		cfg := &HotUpgradeConfig{
			WaitForTasks: false,
			FlushSession: func() error {
				atomic.StoreInt32(&flushed, 1)
				return nil
			},
		}

		// This will succeed through upgrade but fail on RestartWithNewBinary
		// since we can't actually exec in a test. It may also succeed if
		// CanHotRestart returns true and RestartWithNewBinary succeeds (unlikely in test).
		// We just verify the flush callback ran.
		_ = h.PerformHotUpgrade(context.Background(), release, cfg)

		if atomic.LoadInt32(&flushed) != 1 {
			t.Error("FlushSession callback was not called")
		}
	})

	t.Run("flush session error is non-fatal", func(t *testing.T) {
		if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		u := &Upgrader{
			currentVersion: "0.1.0",
			httpClient:     &http.Client{Timeout: 5 * time.Second},
			binaryPath:     binaryPath,
			backupPath:     binaryPath + BackupSuffix,
		}
		g := &GracefulUpgrader{upgrader: u, statePath: statePath, taskChecker: &NoOpTaskChecker{}}
		h := &HotUpgrader{graceful: g, taskChecker: &NoOpTaskChecker{}}

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

		cfg := &HotUpgradeConfig{
			WaitForTasks: false,
			FlushSession: func() error {
				return errors.New("flush failed")
			},
		}

		// Flush error is non-fatal, so upgrade should continue
		_ = h.PerformHotUpgrade(context.Background(), release, cfg)
	})

	t.Run("nil config uses defaults", func(t *testing.T) {
		if err := os.WriteFile(binaryPath, []byte("old-binary"), 0755); err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		u := &Upgrader{
			currentVersion: "0.1.0",
			httpClient:     &http.Client{Timeout: 5 * time.Second},
			binaryPath:     binaryPath,
			backupPath:     binaryPath + BackupSuffix,
		}
		g := &GracefulUpgrader{upgrader: u, statePath: statePath, taskChecker: &NoOpTaskChecker{}}
		h := &HotUpgrader{graceful: g, taskChecker: &NoOpTaskChecker{}}

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

		// nil config should not panic
		_ = h.PerformHotUpgrade(context.Background(), release, nil)
	})
}
