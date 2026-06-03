package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCleanOldLogsSkipsWhenAlreadyRunning covers the TryLock skip-if-running
// guard at the top of cleanOldLogs: if a cleanup is already in progress
// (cleanMu held), a concurrent invocation must return immediately without
// touching any files. We prove this by seeding a backup that the age policy
// would normally delete and asserting it survives a call made while cleanMu is
// held.
func TestCleanOldLogsSkipsWhenAlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	w := &rotatingWriter{
		filename:   logFile,
		maxSize:    1 << 30,
		maxAge:     time.Hour,
		maxBackups: 100,
	}

	// A backup old enough that a real cleanup pass would remove it.
	oldBackup := makeBackup(t, tmpDir, "test", ".log", "old", time.Now().Add(-48*time.Hour))

	// Simulate an in-flight cleanup by holding cleanMu, then invoke cleanOldLogs.
	// TryLock must fail and the call must return without deleting anything.
	w.cleanMu.Lock()
	w.cleanOldLogs()
	w.cleanMu.Unlock()

	if _, err := os.Stat(oldBackup); err != nil {
		t.Errorf("backup must be untouched when a cleanup is already running, stat err = %v", err)
	}

	// Sanity: once the lock is free, a real pass does delete it, proving the
	// file was only spared by the skip guard above (not by some other reason).
	w.cleanOldLogs()
	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Errorf("expected the old backup to be removed by an unguarded pass, stat err = %v", err)
	}
}
