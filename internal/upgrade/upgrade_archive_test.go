package upgrade

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgrader_findAsset(t *testing.T) {
	release := &Release{
		Assets: []Asset{
			{Name: "pilot-darwin-arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin-arm64.tar.gz"},
			{Name: "pilot-darwin-amd64.tar.gz", BrowserDownloadURL: "https://example.com/darwin-amd64.tar.gz"},
			{Name: "pilot-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux-amd64.tar.gz"},
			{Name: "pilot-linux-arm64.tar.gz", BrowserDownloadURL: "https://example.com/linux-arm64.tar.gz"},
		},
	}

	upgrader := &Upgrader{}

	// Test that we can find an asset (the actual OS/arch will be determined at runtime)
	asset := upgrader.findAsset(release)
	// We can't deterministically test which asset is found since it depends on runtime.GOOS/GOARCH
	// but we can ensure the method doesn't panic
	_ = asset
}

func TestUpgrader_findAsset_zip(t *testing.T) {
	// Simulate a release with both tar.gz and zip assets (like GoReleaser produces)
	release := &Release{
		Assets: []Asset{
			{Name: "pilot-darwin-arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin-arm64.tar.gz"},
			{Name: "pilot-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux-amd64.tar.gz"},
			{Name: "pilot-windows-amd64.zip", BrowserDownloadURL: "https://example.com/windows-amd64.zip"},
		},
	}

	upgrader := &Upgrader{}

	// Test that findAsset finds tar.gz for current platform (darwin/linux) or zip for windows
	asset := upgrader.findAsset(release)
	if asset == nil {
		t.Log("No asset found for current platform (expected if running on unsupported platform)")
	}

	// Test explicit matching logic by checking all formats are discoverable
	t.Run("tar.gz preferred over zip", func(t *testing.T) {
		r := &Release{
			Assets: []Asset{
				{Name: "pilot-darwin-arm64.tar.gz", BrowserDownloadURL: "tar"},
				{Name: "pilot-darwin-arm64.zip", BrowserDownloadURL: "zip"},
			},
		}
		a := upgrader.findAsset(r)
		if a != nil && a.BrowserDownloadURL == "zip" {
			t.Error("findAsset should prefer tar.gz over zip")
		}
	})

	t.Run("zip found when no tar.gz", func(t *testing.T) {
		r := &Release{
			Assets: []Asset{
				{Name: "pilot-windows-amd64.zip", BrowserDownloadURL: "https://example.com/win.zip"},
				{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
			},
		}
		// This will only match on windows/amd64, but we verify no panic
		_ = upgrader.findAsset(r)
	})
}

func TestUpgrader_installFromZip(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")

	upgrader := &Upgrader{
		binaryPath: binaryPath,
	}

	// Create a test zip file containing a "pilot" binary
	zipPath := filepath.Join(tempDir, "test.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create zip file: %v", err)
	}

	zw := zip.NewWriter(zipFile)

	// Add a "pilot" entry
	w, err := zw.Create("pilot")
	if err != nil {
		t.Fatalf("Failed to create zip entry: %v", err)
	}
	binaryContent := []byte("test pilot binary content")
	if _, err := w.Write(binaryContent); err != nil {
		t.Fatalf("Failed to write zip entry: %v", err)
	}

	// Add a README (should be ignored)
	w2, err := zw.Create("README.md")
	if err != nil {
		t.Fatalf("Failed to create README entry: %v", err)
	}
	if _, err := w2.Write([]byte("readme")); err != nil {
		t.Fatalf("Failed to write README entry: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("Failed to close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("Failed to close zip file: %v", err)
	}

	// Test extraction
	if err := upgrader.installFromZip(zipPath); err != nil {
		t.Fatalf("installFromZip() error = %v", err)
	}

	// Verify extracted content
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("Failed to read extracted binary: %v", err)
	}
	if string(content) != string(binaryContent) {
		t.Errorf("Extracted content = %q, want %q", string(content), string(binaryContent))
	}
}

func TestUpgrader_installFromZip_exe(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot.exe")

	upgrader := &Upgrader{
		binaryPath: binaryPath,
	}

	// Create a zip with pilot.exe (Windows-style)
	zipPath := filepath.Join(tempDir, "test.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Failed to create zip file: %v", err)
	}

	zw := zip.NewWriter(zipFile)
	w, err := zw.Create("pilot.exe")
	if err != nil {
		t.Fatalf("Failed to create zip entry: %v", err)
	}
	binaryContent := []byte("windows pilot binary")
	if _, err := w.Write(binaryContent); err != nil {
		t.Fatalf("Failed to write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Failed to close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("Failed to close zip file: %v", err)
	}

	if err := upgrader.installFromZip(zipPath); err != nil {
		t.Fatalf("installFromZip() error = %v", err)
	}

	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("Failed to read extracted binary: %v", err)
	}
	if string(content) != string(binaryContent) {
		t.Errorf("Extracted content = %q, want %q", string(content), string(binaryContent))
	}
}

func TestUpgrader_isZip(t *testing.T) {
	tempDir := t.TempDir()
	upgrader := &Upgrader{}

	t.Run("valid zip", func(t *testing.T) {
		zipPath := filepath.Join(tempDir, "test.zip")
		f, _ := os.Create(zipPath)
		zw := zip.NewWriter(f)
		w, _ := zw.Create("dummy")
		_, _ = w.Write([]byte("data"))
		_ = zw.Close()
		_ = f.Close()

		if !upgrader.isZip(zipPath) {
			t.Error("isZip() = false for valid zip file")
		}
	})

	t.Run("not zip", func(t *testing.T) {
		notZipPath := filepath.Join(tempDir, "notzip")
		_ = os.WriteFile(notZipPath, []byte("not a zip file"), 0644)

		if upgrader.isZip(notZipPath) {
			t.Error("isZip() = true for non-zip file")
		}
	})
}
