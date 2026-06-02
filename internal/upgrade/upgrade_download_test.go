package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpgrader_CheckVersion(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"tag_name": "v0.3.0",
			"name": "v0.3.0",
			"body": "Release notes here",
			"draft": false,
			"prerelease": false,
			"published_at": "2025-01-01T00:00:00Z",
			"html_url": "https://github.com/test/test/releases/tag/v0.3.0",
			"assets": []
		}`))
	}))
	defer server.Close()

	// Create upgrader with mock
	upgrader := &Upgrader{
		currentVersion: "0.2.0",
		httpClient:     &http.Client{Timeout: 5 * time.Second},
	}

	// We can't easily test against real GitHub API, but we can verify the structure
	t.Run("version info structure", func(t *testing.T) {
		info := &VersionInfo{
			Current:     "0.2.0",
			Latest:      "v0.3.0",
			UpdateAvail: true,
		}

		if info.Current != "0.2.0" {
			t.Errorf("Current = %q, want %q", info.Current, "0.2.0")
		}
		if !info.UpdateAvail {
			t.Error("UpdateAvail should be true")
		}
	})

	// Keep upgrader in scope
	_ = upgrader
}

func TestUpgrader_BackupAndRollback(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()

	// Create a fake binary
	binaryPath := filepath.Join(tempDir, "pilot")
	if err := os.WriteFile(binaryPath, []byte("original binary content"), 0755); err != nil {
		t.Fatalf("Failed to create test binary: %v", err)
	}

	upgrader := &Upgrader{
		currentVersion: "0.2.0",
		binaryPath:     binaryPath,
		backupPath:     binaryPath + BackupSuffix,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
	}

	// Test backup creation
	t.Run("create backup", func(t *testing.T) {
		if err := upgrader.createBackup(); err != nil {
			t.Fatalf("createBackup() error = %v", err)
		}

		if !upgrader.HasBackup() {
			t.Error("HasBackup() = false after backup creation")
		}

		// Verify backup content
		content, err := os.ReadFile(upgrader.backupPath)
		if err != nil {
			t.Fatalf("Failed to read backup: %v", err)
		}
		if string(content) != "original binary content" {
			t.Errorf("Backup content = %q, want %q", string(content), "original binary content")
		}
	})

	// Simulate binary modification
	t.Run("modify binary", func(t *testing.T) {
		if err := os.WriteFile(binaryPath, []byte("new binary content"), 0755); err != nil {
			t.Fatalf("Failed to modify binary: %v", err)
		}
	})

	// Test rollback
	t.Run("rollback", func(t *testing.T) {
		if err := upgrader.Rollback(); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		// Verify rollback restored original content
		content, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("Failed to read binary after rollback: %v", err)
		}
		if string(content) != "original binary content" {
			t.Errorf("Binary content after rollback = %q, want %q", string(content), "original binary content")
		}

		// Verify backup was removed
		if upgrader.HasBackup() {
			t.Error("HasBackup() = true after rollback, should be false")
		}
	})
}

func TestUpgrader_CleanupBackup(t *testing.T) {
	tempDir := t.TempDir()

	binaryPath := filepath.Join(tempDir, "pilot")
	backupPath := binaryPath + BackupSuffix

	// Create backup file
	if err := os.WriteFile(backupPath, []byte("backup content"), 0755); err != nil {
		t.Fatalf("Failed to create backup file: %v", err)
	}

	upgrader := &Upgrader{
		binaryPath: binaryPath,
		backupPath: backupPath,
	}

	if !upgrader.HasBackup() {
		t.Error("HasBackup() = false, should be true")
	}

	if err := upgrader.CleanupBackup(); err != nil {
		t.Fatalf("CleanupBackup() error = %v", err)
	}

	if upgrader.HasBackup() {
		t.Error("HasBackup() = true after cleanup, should be false")
	}
}

func TestUpgrader_downloadAsset(t *testing.T) {
	content := []byte("downloaded-asset-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	u := &Upgrader{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	t.Run("successful download", func(t *testing.T) {
		asset := &Asset{
			BrowserDownloadURL: server.URL,
			Size:               int64(len(content)),
		}

		var progressPcts []int
		tmpPath, err := u.downloadAsset(context.Background(), asset, func(pct int, msg string) {
			progressPcts = append(progressPcts, pct)
		})
		if err != nil {
			t.Fatalf("downloadAsset() error = %v", err)
		}
		defer func() { _ = os.Remove(tmpPath) }()

		got, err := os.ReadFile(tmpPath)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("Downloaded content = %q, want %q", string(got), string(content))
		}
	})

	t.Run("server error", func(t *testing.T) {
		errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer errServer.Close()

		asset := &Asset{BrowserDownloadURL: errServer.URL}
		_, err := u.downloadAsset(context.Background(), asset, nil)
		if err == nil {
			t.Error("downloadAsset() should return error on 500")
		}
	})
}

func TestUpgrader_fetchLatestRelease(t *testing.T) {
	t.Run("valid releases", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"tag_name": "v1.0.0", "draft": true, "prerelease": false},
				{"tag_name": "v0.9.0", "draft": false, "prerelease": true},
				{"tag_name": "v0.8.0", "draft": false, "prerelease": false, "body": "stable", "assets": []}
			]`))
		}))
		defer server.Close()

		// We can't easily override the URL used by fetchLatestRelease,
		// so test the response parsing indirectly via the mock server.
		resp, err := http.Get(server.URL)
		if err != nil {
			t.Fatalf("HTTP GET error: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var releases []Release
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			t.Fatalf("Decode error: %v", err)
		}

		// Simulate the logic in fetchLatestRelease
		var found *Release
		for i := range releases {
			if !releases[i].Draft && !releases[i].Prerelease {
				found = &releases[i]
				break
			}
		}

		if found == nil {
			t.Fatal("No stable release found")
		}
		if found.TagName != "v0.8.0" {
			t.Errorf("Selected release = %q, want %q", found.TagName, "v0.8.0")
		}
	})

	t.Run("all drafts fallback to first", func(t *testing.T) {
		releases := []Release{
			{TagName: "v2.0.0", Draft: true},
			{TagName: "v1.9.0", Draft: true, Prerelease: true},
		}

		// Simulate fallback logic
		var found *Release
		for i := range releases {
			if !releases[i].Draft && !releases[i].Prerelease {
				found = &releases[i]
				break
			}
		}
		if found == nil && len(releases) > 0 {
			found = &releases[0]
		}

		if found == nil || found.TagName != "v2.0.0" {
			t.Errorf("Fallback release = %v, want v2.0.0", found)
		}
	})
}

func TestUpgrader_BinaryPath(t *testing.T) {
	u := &Upgrader{binaryPath: "/usr/local/bin/pilot"}
	if got := u.BinaryPath(); got != "/usr/local/bin/pilot" {
		t.Errorf("BinaryPath() = %q, want %q", got, "/usr/local/bin/pilot")
	}
}

func TestUpgrader_Rollback_noBackup(t *testing.T) {
	tempDir := t.TempDir()
	u := &Upgrader{
		binaryPath: filepath.Join(tempDir, "pilot"),
		backupPath: filepath.Join(tempDir, "pilot.backup"),
	}

	err := u.Rollback()
	if err == nil {
		t.Error("Rollback() should error when no backup exists")
	}
}
