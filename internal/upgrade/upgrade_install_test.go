package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgrader_isTarGz(t *testing.T) {
	tempDir := t.TempDir()
	u := &Upgrader{}

	t.Run("valid gzip", func(t *testing.T) {
		path := filepath.Join(tempDir, "test.tar.gz")
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)
		hdr := &tar.Header{Name: "pilot", Mode: 0755, Size: 4}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader error: %v", err)
		}
		if _, err := tw.Write([]byte("test")); err != nil {
			t.Fatalf("Write error: %v", err)
		}
		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()

		if !u.isTarGz(path) {
			t.Error("isTarGz() = false for valid gzip file")
		}
	})

	t.Run("not gzip", func(t *testing.T) {
		path := filepath.Join(tempDir, "notgz")
		if err := os.WriteFile(path, []byte("not a gzip file"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		if u.isTarGz(path) {
			t.Error("isTarGz() = true for non-gzip file")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		if u.isTarGz(filepath.Join(tempDir, "nonexistent")) {
			t.Error("isTarGz() = true for nonexistent file")
		}
	})
}

func TestUpgrader_installFromTarGz(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	u := &Upgrader{binaryPath: binaryPath}

	t.Run("extract pilot binary", func(t *testing.T) {
		tarPath := filepath.Join(tempDir, "test.tar.gz")
		f, err := os.Create(tarPath)
		if err != nil {
			t.Fatalf("Create error: %v", err)
		}

		content := []byte("pilot-binary-from-tarball")
		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)

		// Add a non-pilot file first (should be skipped)
		hdr := &tar.Header{Name: "README.md", Mode: 0644, Size: 6, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader error: %v", err)
		}
		if _, err := tw.Write([]byte("readme")); err != nil {
			t.Fatalf("Write error: %v", err)
		}

		// Add the pilot binary
		hdr = &tar.Header{Name: "pilot", Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader error: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("Write error: %v", err)
		}

		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()

		if err := u.installFromTarGz(tarPath); err != nil {
			t.Fatalf("installFromTarGz() error = %v", err)
		}

		got, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("Extracted content = %q, want %q", string(got), string(content))
		}
	})

	t.Run("no pilot binary in archive", func(t *testing.T) {
		tarPath := filepath.Join(tempDir, "nopilot.tar.gz")
		f, err := os.Create(tarPath)
		if err != nil {
			t.Fatalf("Create error: %v", err)
		}

		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)
		hdr := &tar.Header{Name: "other-file", Mode: 0644, Size: 4, Typeflag: tar.TypeReg}
		_ = tw.WriteHeader(hdr)
		_, _ = tw.Write([]byte("data"))
		_ = tw.Close()
		_ = gw.Close()
		_ = f.Close()

		err = u.installFromTarGz(tarPath)
		if err == nil {
			t.Error("installFromTarGz() should error when pilot binary not found")
		}
	})
}

func TestUpgrader_installDirectBinary(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	u := &Upgrader{binaryPath: binaryPath}

	srcPath := filepath.Join(tempDir, "downloaded")
	content := []byte("direct-binary-content")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	if err := u.installDirectBinary(srcPath); err != nil {
		t.Fatalf("installDirectBinary() error = %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("Installed content = %q, want %q", string(got), string(content))
	}
}

func TestUpgrader_installBinary_dispatch(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "pilot")
	u := &Upgrader{binaryPath: binaryPath}

	t.Run("direct binary", func(t *testing.T) {
		srcPath := filepath.Join(tempDir, "plain-binary")
		if err := os.WriteFile(srcPath, []byte("plain"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}

		if err := u.installBinary(srcPath); err != nil {
			t.Fatalf("installBinary() error = %v", err)
		}

		got, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		if string(got) != "plain" {
			t.Errorf("Installed content = %q, want %q", string(got), "plain")
		}
	})
}
