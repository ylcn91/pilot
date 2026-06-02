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
)

// ---------------------------------------------------------------------------
// isHomebrewPath
// ---------------------------------------------------------------------------

func TestIsHomebrewPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/pilot/1.0/bin/pilot", true},
		{"/usr/local/Cellar/pilot/1.0/bin/pilot", true},
		{"/home/linuxbrew/.linuxbrew/Cellar/pilot/1.0/bin/pilot", true},
		{"/usr/local/bin/pilot", false},
		{"/home/user/pilot", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isHomebrewPath(tt.path)
			if got != tt.want {
				t.Errorf("isHomebrewPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// findAsset — extended tests
// ---------------------------------------------------------------------------

func TestFindAsset_MatchesCurrentPlatform(t *testing.T) {
	tarName := fmt.Sprintf("pilot-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	release := &Release{
		Assets: []Asset{
			{Name: tarName, BrowserDownloadURL: "https://example.com/tar"},
			{Name: "pilot-other-other.tar.gz", BrowserDownloadURL: "https://example.com/other"},
		},
	}

	u := &Upgrader{}
	asset := u.findAsset(release)
	if asset == nil {
		t.Fatal("findAsset() returned nil for current platform")
	}
	if asset.Name != tarName {
		t.Errorf("findAsset() name = %q, want %q", asset.Name, tarName)
	}
}

func TestFindAsset_FallbackToBinary(t *testing.T) {
	binaryName := fmt.Sprintf("pilot-%s-%s", runtime.GOOS, runtime.GOARCH)
	release := &Release{
		Assets: []Asset{
			{Name: binaryName, BrowserDownloadURL: "https://example.com/binary"},
		},
	}

	u := &Upgrader{}
	asset := u.findAsset(release)
	if asset == nil {
		t.Fatal("findAsset() returned nil for direct binary asset")
	}
	if asset.Name != binaryName {
		t.Errorf("findAsset() name = %q, want %q", asset.Name, binaryName)
	}
}

func TestFindAsset_NoMatch(t *testing.T) {
	release := &Release{
		Assets: []Asset{
			{Name: "pilot-fakeos-fakearch.tar.gz"},
		},
	}

	u := &Upgrader{}
	if asset := u.findAsset(release); asset != nil {
		t.Errorf("findAsset() = %v, want nil for unmatched platform", asset)
	}
}

func TestFindAsset_EmptyAssets(t *testing.T) {
	u := &Upgrader{}
	if asset := u.findAsset(&Release{}); asset != nil {
		t.Errorf("findAsset() = %v, want nil for empty assets", asset)
	}
}

// ---------------------------------------------------------------------------
// Upgrade end-to-end with mock server
// ---------------------------------------------------------------------------

func TestUpgrade_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	backupPath := binPath + BackupSuffix

	if err := os.WriteFile(binPath, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	newBinary := []byte("new-binary-v2")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(newBinary)))
		_, _ = w.Write(newBinary)
	}))
	defer server.Close()

	u := &Upgrader{
		currentVersion:      "1.0.0",
		httpClient:          server.Client(),
		binaryPath:          binPath,
		backupPath:          backupPath,
		prepareForExecution: func(string) error { return nil },
	}

	release := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{
				Name:               fmt.Sprintf("pilot-%s-%s", runtime.GOOS, runtime.GOARCH),
				BrowserDownloadURL: server.URL + "/pilot",
				Size:               int64(len(newBinary)),
			},
		},
	}

	var progressMessages []string
	err := u.Upgrade(context.Background(), release, func(pct int, msg string) {
		progressMessages = append(progressMessages, msg)
	})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != string(newBinary) {
		t.Errorf("installed content = %q, want %q", got, newBinary)
	}

	if !u.HasBackup() {
		t.Error("backup should exist after upgrade")
	}

	if len(progressMessages) == 0 {
		t.Error("no progress messages reported")
	}
}

func TestUpgrade_NoMatchingAsset(t *testing.T) {
	u := &Upgrader{
		currentVersion: "1.0.0",
		httpClient:     &http.Client{},
	}
	release := &Release{
		TagName: "v2.0.0",
		Assets:  []Asset{{Name: "pilot-fakeos-fakearch.tar.gz"}},
	}

	err := u.Upgrade(context.Background(), release, nil)
	if err == nil {
		t.Fatal("Upgrade() expected error for no matching asset, got nil")
	}
}

func TestUpgrade_WithTarGzAsset(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	backupPath := binPath + BackupSuffix

	if err := os.WriteFile(binPath, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a tar.gz payload
	tarGzPath := filepath.Join(dir, "payload.tar.gz")
	createTestTarGz(t, tarGzPath, "pilot", []byte("tarred-binary"))
	tarGzData, err := os.ReadFile(tarGzPath)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarGzData)))
		_, _ = w.Write(tarGzData)
	}))
	defer server.Close()

	u := &Upgrader{
		currentVersion:      "1.0.0",
		httpClient:          server.Client(),
		binaryPath:          binPath,
		backupPath:          backupPath,
		prepareForExecution: func(string) error { return nil },
	}

	tarName := fmt.Sprintf("pilot-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	release := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{
				Name:               tarName,
				BrowserDownloadURL: server.URL + "/pilot.tar.gz",
				Size:               int64(len(tarGzData)),
			},
		},
	}

	if err := u.Upgrade(context.Background(), release, nil); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != "tarred-binary" {
		t.Errorf("installed content = %q, want %q", got, "tarred-binary")
	}
}

// ---------------------------------------------------------------------------
// BinaryPath / HasBackup / CleanupBackup
// ---------------------------------------------------------------------------

func TestBinaryPath(t *testing.T) {
	u := &Upgrader{binaryPath: "/usr/local/bin/pilot"}
	if got := u.BinaryPath(); got != "/usr/local/bin/pilot" {
		t.Errorf("BinaryPath() = %q, want %q", got, "/usr/local/bin/pilot")
	}
}

func TestCleanupBackup_NoBackup(t *testing.T) {
	dir := t.TempDir()
	u := &Upgrader{backupPath: filepath.Join(dir, "nonexistent.backup")}
	if err := u.CleanupBackup(); err != nil {
		t.Fatalf("CleanupBackup() error = %v", err)
	}
}
