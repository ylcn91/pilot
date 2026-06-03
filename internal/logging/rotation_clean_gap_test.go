package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeBackup creates a backup file matching the rotation glob pattern
// (prefix.<token>.ext) and sets its mod time deterministically.
func makeBackup(t *testing.T, dir, prefix, ext, token string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, prefix+"."+token+ext)
	if err := os.WriteFile(path, []byte("backup\n"), 0644); err != nil {
		t.Fatalf("write backup %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

// TestCleanOldLogsAgeBasedRemoval verifies cleanOldLogs deletes backups older
// than maxAge while keeping recent ones. Mod times are controlled via
// os.Chtimes so the test is deterministic and does not rely on wall-clock
// progression.
func TestCleanOldLogsAgeBasedRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	w := &rotatingWriter{
		filename:   logFile,
		maxSize:    1 << 30,
		maxAge:     24 * time.Hour,
		maxBackups: 100, // high so only age governs removal
	}

	now := time.Now()
	oldBackup := makeBackup(t, tmpDir, "test", ".log", "old", now.Add(-48*time.Hour))
	recentBackup := makeBackup(t, tmpDir, "test", ".log", "recent", now.Add(-1*time.Hour))

	w.cleanOldLogs()

	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Errorf("expected old backup (48h) to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(recentBackup); err != nil {
		t.Errorf("expected recent backup (1h) to be kept, stat err = %v", err)
	}
}

// TestCleanOldLogsMaxBackupsEviction verifies cleanOldLogs removes the oldest
// backups first once the count exceeds maxBackups, while all are within the
// age window.
func TestCleanOldLogsMaxBackupsEviction(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	w := &rotatingWriter{
		filename:   logFile,
		maxSize:    1 << 30,
		maxAge:     365 * 24 * time.Hour, // huge so age never triggers removal
		maxBackups: 2,
	}

	now := time.Now()
	// Three backups, all recent, with distinct mod times.
	b1 := makeBackup(t, tmpDir, "test", ".log", "a", now.Add(-3*time.Hour)) // oldest -> evicted
	b2 := makeBackup(t, tmpDir, "test", ".log", "b", now.Add(-2*time.Hour))
	b3 := makeBackup(t, tmpDir, "test", ".log", "c", now.Add(-1*time.Hour))

	w.cleanOldLogs()

	if _, err := os.Stat(b1); !os.IsNotExist(err) {
		t.Errorf("expected oldest backup to be evicted beyond maxBackups, stat err = %v", err)
	}
	if _, err := os.Stat(b2); err != nil {
		t.Errorf("expected 2nd-newest backup to be kept, stat err = %v", err)
	}
	if _, err := os.Stat(b3); err != nil {
		t.Errorf("expected newest backup to be kept, stat err = %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(tmpDir, "test.*.log"))
	if len(matches) != w.maxBackups {
		t.Errorf("expected %d backups remaining, got %d: %v", w.maxBackups, len(matches), matches)
	}
}

// TestCleanOldLogsNeverRemovesActiveFile verifies the active log file is never
// deleted by cleanOldLogs even when it matches the glob and is old.
func TestCleanOldLogsNeverRemovesActiveFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	if err := os.WriteFile(logFile, []byte("active\n"), 0644); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	w := &rotatingWriter{
		filename:   logFile,
		maxSize:    1 << 30,
		maxAge:     time.Hour,
		maxBackups: 0,
	}

	w.cleanOldLogs()

	if _, err := os.Stat(logFile); err != nil {
		t.Errorf("active log file must not be removed by cleanOldLogs, stat err = %v", err)
	}
}
