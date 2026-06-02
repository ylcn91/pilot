package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// installBinary installs the new binary from a downloaded file
func (u *Upgrader) installBinary(downloadPath string) error {
	var err error

	// Check archive format and extract accordingly
	if strings.HasSuffix(downloadPath, ".tar.gz") || u.isTarGz(downloadPath) {
		err = u.installFromTarGz(downloadPath)
	} else if strings.HasSuffix(downloadPath, ".zip") || u.isZip(downloadPath) {
		err = u.installFromZip(downloadPath)
	} else {
		// Direct binary
		err = u.installDirectBinary(downloadPath)
	}

	if err != nil {
		return err
	}

	// On Darwin, a codesign failure means Gatekeeper will kill the binary on
	// first launch — surface it so callers can transition to UpgradeStateFailed.
	prepare := PrepareForExecution
	if u.prepareForExecution != nil {
		prepare = u.prepareForExecution
	}
	if err := prepare(u.binaryPath); err != nil {
		return fmt.Errorf("codesign failed — macOS Gatekeeper may block this binary "+
			"(run: xattr -d com.apple.quarantine %s): %w", u.binaryPath, err)
	}

	return nil
}

// installToBinaryPath writes a new binary beside the current executable and
// swaps it into place once the write completes. This avoids truncating a
// running executable on Unix-like systems, which returns ETXTBSY.
// expectedSize is the number of bytes the write callback must produce; pass 0
// to skip the check (no caller currently needs this).
func (u *Upgrader) installToBinaryPath(write func(io.Writer) error, expectedSize int64) error {
	dir := filepath.Dir(u.binaryPath)
	tempFile, err := os.CreateTemp(dir, ".pilot-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp binary: %w", err)
	}

	tempPath := tempFile.Name()
	closed := false
	cleanup := true
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0755); err != nil {
		return fmt.Errorf("failed to set temp binary permissions: %w", err)
	}

	if err := write(tempFile); err != nil {
		return err
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close binary: %w", err)
	}
	closed = true

	if expectedSize > 0 {
		fi, err := os.Stat(tempPath)
		if err != nil {
			return fmt.Errorf("failed to stat written binary: %w", err)
		}
		if fi.Size() != expectedSize {
			return fmt.Errorf("truncated write: wrote %d bytes, expected %d", fi.Size(), expectedSize)
		}
	}

	if runtime.GOOS == "windows" {
		if err := os.Remove(u.binaryPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing binary: %w", err)
		}
	}

	if err := os.Rename(tempPath, u.binaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	cleanup = false
	return nil
}

// isTarGz checks if a file is a gzipped tarball
func (u *Upgrader) isTarGz(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// Check magic bytes for gzip
	buf := make([]byte, 2)
	if _, err := f.Read(buf); err != nil {
		return false
	}
	return buf[0] == 0x1f && buf[1] == 0x8b
}

// isZip checks if a file is a zip archive
func (u *Upgrader) isZip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// Check magic bytes for zip (PK\x03\x04)
	buf := make([]byte, 4)
	if _, err := f.Read(buf); err != nil {
		return false
	}
	return buf[0] == 0x50 && buf[1] == 0x4b && buf[2] == 0x03 && buf[3] == 0x04
}

// installFromTarGz extracts and installs from a tarball
func (u *Upgrader) installFromTarGz(tarPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)

	// Find the binary in the tarball
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary not found in archive")
		}
		if err != nil {
			return fmt.Errorf("failed to read tarball: %w", err)
		}

		// Look for the pilot binary (pilot or pilot.exe)
		baseName := filepath.Base(header.Name)
		if header.Typeflag == tar.TypeReg &&
			(baseName == "pilot" || baseName == "pilot.exe") {
			return u.installToBinaryPath(func(out io.Writer) error {
				if _, err := io.Copy(out, tr); err != nil {
					return fmt.Errorf("failed to extract binary: %w", err)
				}
				return nil
			}, header.Size)
		}
	}
}

// installFromZip extracts and installs from a zip archive
func (u *Upgrader) installFromZip(zipPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	// Find the pilot binary in the zip
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(f.Name)
		// Match "pilot" or "pilot.exe"
		if name == "pilot" || name == "pilot.exe" {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("failed to open zip entry: %w", err)
			}
			defer func() { _ = rc.Close() }()

			return u.installToBinaryPath(func(out io.Writer) error {
				if _, err := io.Copy(out, rc); err != nil {
					return fmt.Errorf("failed to extract binary: %w", err)
				}
				return nil
			}, int64(f.UncompressedSize64))
		}
	}

	return fmt.Errorf("binary not found in zip archive")
}

// installDirectBinary installs a direct binary file
func (u *Upgrader) installDirectBinary(srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source binary: %w", err)
	}

	return u.installToBinaryPath(func(dst io.Writer) error {
		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
		return nil
	}, srcInfo.Size())
}
