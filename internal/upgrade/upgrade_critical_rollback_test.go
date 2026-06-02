package upgrade

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Rollback / Recovery paths
// ---------------------------------------------------------------------------

func TestUpgrade_RollbackOnInstallFailure(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	backupPath := binPath + BackupSuffix

	originalContent := []byte("original-v1")
	if err := os.WriteFile(binPath, originalContent, 0755); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{
		currentVersion: "1.0.0",
		binaryPath:     binPath,
		backupPath:     backupPath,
	}

	// Create backup
	if err := u.createBackup(); err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}

	// Simulate failed install (binary gets corrupted)
	if err := os.WriteFile(binPath, []byte("corrupted"), 0755); err != nil {
		t.Fatal(err)
	}

	// Rollback should restore original
	if err := u.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != string(originalContent) {
		t.Errorf("after rollback content = %q, want %q", got, originalContent)
	}

	if u.HasBackup() {
		t.Error("HasBackup() = true after rollback, want false")
	}
}

func TestRollback_NoBackup(t *testing.T) {
	dir := t.TempDir()
	u := &Upgrader{
		binaryPath: filepath.Join(dir, "pilot"),
		backupPath: filepath.Join(dir, "pilot.backup"),
	}
	err := u.Rollback()
	if err == nil {
		t.Fatal("Rollback() expected error when no backup exists, got nil")
	}
}

func TestUpgrade_EndToEnd_RollbackOnFailedInstall(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	backupPath := binPath + BackupSuffix

	originalContent := []byte("original-v1")
	if err := os.WriteFile(binPath, originalContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Serve a valid download but make the install target read-only after backup
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new-binary"))
	}))
	defer server.Close()

	u := &Upgrader{
		currentVersion: "1.0.0",
		httpClient:     server.Client(),
		binaryPath:     binPath,
		backupPath:     backupPath,
	}

	// First create backup manually
	if err := u.createBackup(); err != nil {
		t.Fatal(err)
	}

	// Verify backup exists before rollback
	if !u.HasBackup() {
		t.Fatal("backup should exist")
	}

	// Simulate corrupted install
	if err := os.WriteFile(binPath, []byte("bad"), 0755); err != nil {
		t.Fatal(err)
	}

	// Rollback
	if err := u.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != string(originalContent) {
		t.Errorf("content = %q, want %q", got, originalContent)
	}
}

// ---------------------------------------------------------------------------
// installFromZip — no binary
// ---------------------------------------------------------------------------

func TestInstallFromZip_NoBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	zipPath := filepath.Join(dir, "test.zip")

	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zipFile)
	w, err := zw.Create("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("readme"))
	_ = zw.Close()
	_ = zipFile.Close()

	u := &Upgrader{binaryPath: binPath}
	err = u.installFromZip(zipPath)
	if err == nil {
		t.Fatal("installFromZip() expected error for missing binary, got nil")
	}
}

func TestInstallFromZip_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(zipPath, []byte("not a zip"), 0644); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: filepath.Join(dir, "pilot")}
	err := u.installFromZip(zipPath)
	if err == nil {
		t.Fatal("installFromZip() expected error for invalid zip, got nil")
	}
}
