package upgrade

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// installBinary dispatch
// ---------------------------------------------------------------------------

func TestInstallBinary_DirectBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	srcPath := filepath.Join(dir, "downloaded")
	if err := os.WriteFile(srcPath, []byte("new-binary"), 0644); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath, prepareForExecution: func(string) error { return nil }}
	if err := u.installBinary(srcPath); err != nil {
		t.Fatalf("installBinary() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != "new-binary" {
		t.Errorf("installed content = %q, want %q", got, "new-binary")
	}

	info, _ := os.Stat(binPath)
	if info.Mode()&0755 != 0755 {
		t.Errorf("installed permissions = %o, want 0755", info.Mode()&os.ModePerm)
	}
}

func TestInstallBinary_TarGz(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	tarPath := filepath.Join(dir, "update.tar.gz")

	createTestTarGz(t, tarPath, "pilot", []byte("tar-binary-content"))

	u := &Upgrader{binaryPath: binPath, prepareForExecution: func(string) error { return nil }}
	if err := u.installBinary(tarPath); err != nil {
		t.Fatalf("installBinary() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != "tar-binary-content" {
		t.Errorf("installed content = %q, want %q", got, "tar-binary-content")
	}
}

// ---------------------------------------------------------------------------
// isTarGz
// ---------------------------------------------------------------------------

func TestIsTarGz_ValidGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.tar.gz")
	createTestTarGz(t, path, "pilot", []byte("content"))

	u := &Upgrader{}
	if !u.isTarGz(path) {
		t.Error("isTarGz() = false for valid gzip file")
	}
}

func TestIsTarGz_NotGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notgz")
	if err := os.WriteFile(path, []byte("plain text file"), 0644); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{}
	if u.isTarGz(path) {
		t.Error("isTarGz() = true for non-gzip file")
	}
}

func TestIsTarGz_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{}
	if u.isTarGz(path) {
		t.Error("isTarGz() = true for empty file")
	}
}

func TestIsTarGz_NonexistentFile(t *testing.T) {
	u := &Upgrader{}
	if u.isTarGz("/nonexistent/path/file.tar.gz") {
		t.Error("isTarGz() = true for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// installFromTarGz
// ---------------------------------------------------------------------------

func TestInstallFromTarGz_Success(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	tarPath := filepath.Join(dir, "release.tar.gz")
	content := []byte("pilot-binary-from-tar")

	createTestTarGz(t, tarPath, "pilot", content)

	u := &Upgrader{binaryPath: binPath}
	if err := u.installFromTarGz(tarPath); err != nil {
		t.Fatalf("installFromTarGz() error = %v", err)
	}

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("failed to read installed binary: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestInstallFromTarGz_NestedPath(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	tarPath := filepath.Join(dir, "release.tar.gz")

	createTestTarGz(t, tarPath, "dist/pilot", []byte("nested-binary"))

	u := &Upgrader{binaryPath: binPath}
	if err := u.installFromTarGz(tarPath); err != nil {
		t.Fatalf("installFromTarGz() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != "nested-binary" {
		t.Errorf("content = %q, want %q", got, "nested-binary")
	}
}

func TestInstallFromTarGz_NoBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	tarPath := filepath.Join(dir, "release.tar.gz")

	createTestTarGz(t, tarPath, "README.md", []byte("readme"))

	u := &Upgrader{binaryPath: binPath}
	err := u.installFromTarGz(tarPath)
	if err == nil {
		t.Fatal("installFromTarGz() expected error for missing binary, got nil")
	}
}

func TestInstallFromTarGz_CorruptedArchive(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "corrupt.tar.gz")
	// Write gzip magic bytes followed by garbage
	if err := os.WriteFile(tarPath, []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0xff}, 0644); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: filepath.Join(dir, "pilot")}
	err := u.installFromTarGz(tarPath)
	if err == nil {
		t.Fatal("installFromTarGz() expected error for corrupted archive, got nil")
	}
}

func TestInstallFromTarGz_NonexistentFile(t *testing.T) {
	u := &Upgrader{binaryPath: "/tmp/pilot"}
	err := u.installFromTarGz("/nonexistent/file.tar.gz")
	if err == nil {
		t.Fatal("installFromTarGz() expected error for nonexistent file, got nil")
	}
}

func TestInstallFromTarGz_PilotExe(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	tarPath := filepath.Join(dir, "release.tar.gz")

	createTestTarGz(t, tarPath, "pilot.exe", []byte("windows-binary"))

	u := &Upgrader{binaryPath: binPath}
	if err := u.installFromTarGz(tarPath); err != nil {
		t.Fatalf("installFromTarGz() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != "windows-binary" {
		t.Errorf("content = %q, want %q", got, "windows-binary")
	}
}

// ---------------------------------------------------------------------------
// installDirectBinary
// ---------------------------------------------------------------------------

func TestInstallDirectBinary_Success(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "downloaded")
	binPath := filepath.Join(dir, "pilot")

	content := []byte("direct-binary-content")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath}
	if err := u.installDirectBinary(srcPath); err != nil {
		t.Fatalf("installDirectBinary() error = %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	info, _ := os.Stat(binPath)
	if info.Mode()&0755 != 0755 {
		t.Errorf("permissions = %o, want 0755", info.Mode()&os.ModePerm)
	}
}

func TestInstallDirectBinary_ReplacesExistingBinaryAtomically(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "downloaded")
	binPath := filepath.Join(dir, "pilot")

	if err := os.WriteFile(srcPath, []byte("new-binary"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	before, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath}
	if err := u.installDirectBinary(srcPath); err != nil {
		t.Fatalf("installDirectBinary() error = %v", err)
	}

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("failed to read installed binary: %v", err)
	}
	if string(got) != "new-binary" {
		t.Errorf("content = %q, want %q", got, "new-binary")
	}

	after, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("installDirectBinary() rewrote existing inode, want atomic replacement")
	}
}

func TestInstallToBinaryPath_CleansUpTempOnWriterError(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	if err := os.WriteFile(binPath, []byte("old-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath}
	errBoom := fmt.Errorf("boom")
	err := u.installToBinaryPath(func(out io.Writer) error {
		_, _ = out.Write([]byte("partial"))
		return errBoom
	}, 0)
	if err == nil {
		t.Fatal("installToBinaryPath() expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("installToBinaryPath() error = %v, want wrapped %v", err, errBoom)
	}

	got, readErr := os.ReadFile(binPath)
	if readErr != nil {
		t.Fatalf("failed to read existing binary: %v", readErr)
	}
	if string(got) != "old-binary" {
		t.Errorf("content = %q, want %q", got, "old-binary")
	}

	matches, globErr := filepath.Glob(filepath.Join(dir, ".pilot-install-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temp install files left behind: %v", matches)
	}
}

func TestInstallToBinaryPath_TruncatedWrite(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "pilot")
	if err := os.WriteFile(binPath, []byte("original-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	u := &Upgrader{binaryPath: binPath}
	err := u.installToBinaryPath(func(out io.Writer) error {
		_, _ = out.Write([]byte("short"))
		return nil
	}, 100)
	if err == nil {
		t.Fatal("installToBinaryPath() expected error for truncated write, got nil")
	}

	// Original binary must be preserved.
	got, readErr := os.ReadFile(binPath)
	if readErr != nil {
		t.Fatalf("failed to read existing binary: %v", readErr)
	}
	if string(got) != "original-binary" {
		t.Errorf("content = %q, want %q", got, "original-binary")
	}

	// Temp file must be cleaned up.
	matches, globErr := filepath.Glob(filepath.Join(dir, ".pilot-install-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temp install files left behind: %v", matches)
	}
}

func TestInstallDirectBinary_SourceMissing(t *testing.T) {
	dir := t.TempDir()
	u := &Upgrader{binaryPath: filepath.Join(dir, "pilot")}
	err := u.installDirectBinary(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Fatal("installDirectBinary() expected error for missing source, got nil")
	}
}

func TestInstallDirectBinary_ReadOnlyDest(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src")
	if err := os.WriteFile(srcPath, []byte("bin"), 0644); err != nil {
		t.Fatal(err)
	}

	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(readOnlyDir, 0755) }()

	u := &Upgrader{binaryPath: filepath.Join(readOnlyDir, "pilot")}
	err := u.installDirectBinary(srcPath)
	if err == nil {
		t.Fatal("installDirectBinary() expected permission error, got nil")
	}
}
