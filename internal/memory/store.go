package memory

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides persistent storage for Pilot using SQLite.
// It manages executions, patterns, projects, and cross-project learning data.
// Store handles database migrations automatically on initialization.
type Store struct {
	db   *sql.DB
	path string

	logSubMu       sync.RWMutex
	logSubscribers map[chan *LogEntry]struct{}

	// usageThresholds holds the monthly alert thresholds evaluated by
	// CheckUsageThresholds. Defaulted in NewStore to the historical $100 cost
	// and 500-task limits; kept as a field so they can be made configurable.
	usageThresholds []UsageThreshold
}

// NewStore creates a new Store instance with a SQLite database at the given path.
// It creates the data directory if it does not exist and runs database migrations.
// Returns an error if the database cannot be opened or migrations fail.
func NewStore(dataPath string) (*Store, error) {
	// Ensure directory exists
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataPath, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode, busy timeout, and foreign key enforcement.
	// Foreign keys default to OFF in SQLite; ON DELETE CASCADE on pattern_projects
	// and pattern_feedback only fires once this pragma is set.
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=10000; PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("failed to set database pragmas: %w", err)
	}

	// SQLite supports only one writer at a time. Limiting to 1 open connection
	// serializes all database access, eliminating SQLITE_BUSY contention.
	// WAL mode still allows the single connection to interleave reads and writes efficiently.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Don't close idle connections

	store := &Store{
		db:              db,
		path:            dataPath,
		logSubscribers:  make(map[chan *LogEntry]struct{}),
		usageThresholds: defaultUsageThresholds(),
	}

	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

// DB returns the underlying *sql.DB for sharing with other packages (e.g., teams store).
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the database connection and releases resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// withRetry executes a database operation with exponential backoff on transient errors.
// Retries up to 5 times with 100ms, 200ms, 400ms, 800ms, 1600ms delays.
func (s *Store) withRetry(operation string, fn func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		// Only retry on SQLITE_BUSY/SQLITE_LOCKED
		errStr := strings.ToLower(err.Error())
		if !strings.Contains(errStr, "database is locked") &&
			!strings.Contains(errStr, "sqlite_busy") &&
			!strings.Contains(errStr, "sqlite_locked") {
			return err // Non-retryable error
		}
		delay := time.Duration(100<<uint(attempt)) * time.Millisecond
		slog.Warn("Database locked, retrying",
			slog.String("operation", operation),
			slog.Int("attempt", attempt+1),
			slog.Duration("delay", delay),
		)
		time.Sleep(delay)
	}
	return fmt.Errorf("%s failed after 5 retries: %w", operation, err)
}
