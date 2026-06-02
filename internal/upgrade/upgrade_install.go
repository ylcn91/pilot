package upgrade

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
)

// Upgrade downloads and installs the latest version
func (u *Upgrader) Upgrade(ctx context.Context, release *Release, onProgress func(pct int, msg string)) error {
	// Find appropriate asset for current platform
	asset := u.findAsset(release)
	if asset == nil {
		return fmt.Errorf("no release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if onProgress != nil {
		onProgress(0, "Downloading update...")
	}

	// Download to temp file
	tempFile, err := u.downloadAsset(ctx, asset, onProgress)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer func() { _ = os.Remove(tempFile) }()

	if onProgress != nil {
		onProgress(70, "Creating backup...")
	}

	// Backup current binary
	if err := u.createBackup(); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	if onProgress != nil {
		onProgress(80, "Installing update...")
	}

	// Install new binary
	if err := u.installBinary(tempFile); err != nil {
		// Attempt rollback
		if rollbackErr := u.Rollback(); rollbackErr != nil {
			return fmt.Errorf("install failed: %w; rollback also failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("install failed (rolled back): %w", err)
	}

	if onProgress != nil {
		onProgress(100, "Update complete!")
	}

	return nil
}

// Rollback restores the previous version
func (u *Upgrader) Rollback() error {
	if _, err := os.Stat(u.backupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup found at %s", u.backupPath)
	}

	// Remove current binary
	if err := os.Remove(u.binaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove current binary: %w", err)
	}

	// Restore backup
	if err := os.Rename(u.backupPath, u.binaryPath); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	return nil
}

// CleanupBackup removes the backup file
func (u *Upgrader) CleanupBackup() error {
	if err := os.Remove(u.backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove backup: %w", err)
	}
	return nil
}

// HasBackup checks if a backup exists
func (u *Upgrader) HasBackup() bool {
	_, err := os.Stat(u.backupPath)
	return err == nil
}

// BinaryPath returns the path to the current binary
func (u *Upgrader) BinaryPath() string {
	return u.binaryPath
}

// findAsset finds the appropriate release asset for the current platform
func (u *Upgrader) findAsset(release *Release) *Asset {
	// Expected asset name format: pilot-{os}-{arch}.tar.gz (or .zip for Windows)
	expectedTarGz := fmt.Sprintf("pilot-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	expectedZip := fmt.Sprintf("pilot-%s-%s.zip", runtime.GOOS, runtime.GOARCH)

	// Try .tar.gz first (preferred for Unix platforms)
	for i := range release.Assets {
		if release.Assets[i].Name == expectedTarGz {
			return &release.Assets[i]
		}
	}

	// Try .zip (Windows releases use zip instead of tar.gz)
	for i := range release.Assets {
		if release.Assets[i].Name == expectedZip {
			return &release.Assets[i]
		}
	}

	// Try without extension
	expectedBinary := fmt.Sprintf("pilot-%s-%s", runtime.GOOS, runtime.GOARCH)
	for i := range release.Assets {
		if release.Assets[i].Name == expectedBinary {
			return &release.Assets[i]
		}
	}

	return nil
}

// downloadAsset downloads a release asset to a temp file
func (u *Upgrader) downloadAsset(ctx context.Context, asset *Asset, onProgress func(pct int, msg string)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create temp file
	tempFile, err := os.CreateTemp("", "pilot-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	tempFileName := tempFile.Name()
	closeFile := func() {
		_ = tempFile.Close()
	}

	// Download with progress tracking
	written := int64(0)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tempFile.Write(buf[:n]); writeErr != nil {
				closeFile()
				_ = os.Remove(tempFileName)
				return "", fmt.Errorf("failed to write: %w", writeErr)
			}
			written += int64(n)
			if onProgress != nil && asset.Size > 0 {
				pct := int(float64(written) / float64(asset.Size) * 60) // Scale to 60% of progress
				onProgress(pct, fmt.Sprintf("Downloading... %d%%", pct))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			closeFile()
			_ = os.Remove(tempFileName)
			return "", fmt.Errorf("failed to read: %w", readErr)
		}
	}

	closeFile()
	return tempFileName, nil
}

// createBackup creates a backup of the current binary
func (u *Upgrader) createBackup() error {
	// Remove existing backup if any
	if err := os.Remove(u.backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old backup: %w", err)
	}

	// Copy current binary to backup
	src, err := os.Open(u.binaryPath)
	if err != nil {
		return fmt.Errorf("failed to open current binary: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(u.backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	return nil
}
