package upgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// downloadAsset
// ---------------------------------------------------------------------------

func TestDownloadAsset_Success(t *testing.T) {
	payload := []byte("binary-content-here")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	u := &Upgrader{httpClient: server.Client()}
	asset := &Asset{
		BrowserDownloadURL: server.URL + "/pilot.tar.gz",
		Size:               int64(len(payload)),
	}

	var progressCalled bool
	tmpPath, err := u.downloadAsset(context.Background(), asset, func(pct int, msg string) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("downloadAsset() error = %v", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded content = %q, want %q", got, payload)
	}
	if !progressCalled {
		t.Error("progress callback was not invoked")
	}
}

func TestDownloadAsset_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	u := &Upgrader{httpClient: server.Client()}
	asset := &Asset{BrowserDownloadURL: server.URL + "/missing", Size: 100}

	_, err := u.downloadAsset(context.Background(), asset, nil)
	if err == nil {
		t.Fatal("downloadAsset() expected error for 404, got nil")
	}
}

func TestDownloadAsset_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = 50 * time.Millisecond

	u := &Upgrader{httpClient: client}
	asset := &Asset{BrowserDownloadURL: server.URL + "/slow", Size: 100}

	_, err := u.downloadAsset(context.Background(), asset, nil)
	if err == nil {
		t.Fatal("downloadAsset() expected timeout error, got nil")
	}
}

func TestDownloadAsset_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	u := &Upgrader{httpClient: server.Client()}
	asset := &Asset{BrowserDownloadURL: server.URL + "/slow", Size: 100}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := u.downloadAsset(ctx, asset, nil)
	if err == nil {
		t.Fatal("downloadAsset() expected context canceled error, got nil")
	}
}

func TestDownloadAsset_NilProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	u := &Upgrader{httpClient: server.Client()}
	asset := &Asset{BrowserDownloadURL: server.URL, Size: 0}

	tmpPath, err := u.downloadAsset(context.Background(), asset, nil)
	if err != nil {
		t.Fatalf("downloadAsset() error = %v", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()
}

// ---------------------------------------------------------------------------
// createBackup
// ---------------------------------------------------------------------------

func TestCreateBackup_Success(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	content := []byte("original-binary")
	if err := os.WriteFile(binPath, content, 0755); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath, backupPath: binPath + BackupSuffix}
	if err := u.createBackup(); err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}

	got, err := os.ReadFile(u.backupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("backup content = %q, want %q", got, content)
	}
}

func TestCreateBackup_ReplacesExistingBackup(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	backupPath := binPath + BackupSuffix

	if err := os.WriteFile(binPath, []byte("v2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("old-backup"), 0755); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath, backupPath: backupPath}
	if err := u.createBackup(); err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}

	got, _ := os.ReadFile(backupPath)
	if string(got) != "v2" {
		t.Errorf("backup content = %q, want %q", got, "v2")
	}
}

func TestCreateBackup_SourceMissing(t *testing.T) {
	dir := t.TempDir()
	u := &Upgrader{
		binaryPath: filepath.Join(dir, "nonexistent"),
		backupPath: filepath.Join(dir, "nonexistent.backup"),
	}
	if err := u.createBackup(); err == nil {
		t.Fatal("createBackup() expected error for missing source, got nil")
	}
}

func TestCreateBackup_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	if err := os.WriteFile(binPath, []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}

	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnlyDir, 0755) }()

	u := &Upgrader{
		binaryPath: binPath,
		backupPath: filepath.Join(readOnlyDir, "pilot.backup"),
	}
	err := u.createBackup()
	if err == nil {
		t.Fatal("createBackup() expected permission error, got nil")
	}
}
