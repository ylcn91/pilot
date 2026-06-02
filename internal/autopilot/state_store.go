package autopilot

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// StateStore persists autopilot state to SQLite for crash recovery.
// It stores PR lifecycle state and processed issue tracking so that
// autopilot can resume from the correct stage after a restart.
type StateStore struct {
	db *sql.DB
}

// NewStateStore creates a StateStore using an existing *sql.DB connection.
// It runs migrations to create the required tables if they don't exist.
func NewStateStore(db *sql.DB) (*StateStore, error) {
	s := &StateStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("autopilot state store migration failed: %w", err)
	}
	return s, nil
}

// NewStateStoreFromPath creates a StateStore by opening a new SQLite connection.
// Used primarily for testing with in-memory databases (path = ":memory:").
func NewStateStoreFromPath(path string) (*StateStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("failed to set database pragmas: %w", err)
	}
	return NewStateStore(db)
}

func (s *StateStore) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS autopilot_pr_state (
			pr_number INTEGER PRIMARY KEY,
			pr_url TEXT NOT NULL,
			issue_number INTEGER DEFAULT 0,
			branch_name TEXT NOT NULL DEFAULT '',
			head_sha TEXT DEFAULT '',
			stage TEXT NOT NULL,
			ci_status TEXT NOT NULL DEFAULT 'pending',
			last_checked DATETIME,
			ci_wait_started_at DATETIME,
			merge_attempts INTEGER DEFAULT 0,
			error TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_version TEXT DEFAULT '',
			release_bump_type TEXT DEFAULT ''
		)`,
		// GH-2345: Track whether the merge-completion comment has been posted,
		// so re-entry into StageMerging (e.g. after crash recovery) does not
		// emit duplicate "PR merged" comments on the linked issue.
		`ALTER TABLE autopilot_pr_state ADD COLUMN merge_notification_posted INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS autopilot_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS autopilot_pr_failures (
			pr_number INTEGER PRIMARY KEY,
			failure_count INTEGER NOT NULL DEFAULT 0,
			last_failure_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// GH-1838: Generic adapter_processed table — replaces 7 per-adapter tables.
		// Source and repo form the namespace; repo is '' for tracker-style adapters.
		`CREATE TABLE IF NOT EXISTS adapter_processed (
			adapter TEXT NOT NULL,
			issue_id TEXT NOT NULL,
			processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT '',
			PRIMARY KEY (adapter, issue_id)
		)`,
		// GH-2685: Async approval state — persisted so crash-recovery can resume
		// the non-blocking tick handler without re-submitting the request.
		`ALTER TABLE autopilot_pr_state ADD COLUMN approval_request_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE autopilot_pr_state ADD COLUMN approval_decision TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE autopilot_pr_state ADD COLUMN approval_requested_at DATETIME`,
		// GH-2717: Non-blocking post-merge CI — persist SHA and start time so
		// daemon restarts resume monitoring the same commit without re-fetching.
		`ALTER TABLE autopilot_pr_state ADD COLUMN post_merge_sha TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE autopilot_pr_state ADD COLUMN post_merge_ci_started_at DATETIME`,
		// TASK-298: Add repo column to adapter_processed for cross-repo dedup (TASK-288 Step 2).
		// Repo defaults to '' for tracker-style adapters (linear, jira, asana, etc.).
		`ALTER TABLE adapter_processed ADD COLUMN repo TEXT NOT NULL DEFAULT ''`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			// Ignore "duplicate column" errors from ALTER TABLE migrations
			if strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// TASK-298: Consolidate 7 legacy per-adapter tables into adapter_processed.
	if err := s.migrateLegacyProcessedTables(); err != nil {
		return fmt.Errorf("legacy processed tables migration failed: %w", err)
	}

	return nil
}

// migrateLegacyProcessedTables copies rows from the 7 legacy per-adapter tables
// into adapter_processed, then drops the legacy tables.
// Safe to run multiple times: checks table existence and uses INSERT OR IGNORE.
func (s *StateStore) migrateLegacyProcessedTables() error {
	type legacyTable struct {
		table   string
		adapter string
		idCol   string
		castInt bool // true when the PK column is INTEGER and must be cast to TEXT
	}
	tables := []legacyTable{
		{"autopilot_processed", "github", "issue_number", true},
		{"linear_processed", "linear", "issue_id", false},
		{"gitlab_processed", "gitlab", "issue_number", true},
		{"jira_processed", "jira", "issue_key", false},
		{"asana_processed", "asana", "task_gid", false},
		{"azuredevops_processed", "azuredevops", "work_item_id", true},
		{"plane_processed", "plane", "issue_id", false},
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, lt := range tables {
		// Skip if legacy table does not exist (fresh install or already dropped).
		var exists int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, lt.table,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check table %s: %w", lt.table, err)
		}
		if exists == 0 {
			continue
		}

		idExpr := lt.idCol
		if lt.castInt {
			idExpr = fmt.Sprintf("CAST(%s AS TEXT)", lt.idCol)
		}

		// Copy rows; OR IGNORE skips rows already present in adapter_processed.
		q := fmt.Sprintf(`
			INSERT OR IGNORE INTO adapter_processed (adapter, repo, issue_id, processed_at, result)
			SELECT ?, '', %s, processed_at, COALESCE(result, '') FROM %s
		`, idExpr, lt.table)
		if _, err := tx.Exec(q, lt.adapter); err != nil {
			return fmt.Errorf("copy %s: %w", lt.table, err)
		}

		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + lt.table); err != nil {
			return fmt.Errorf("drop %s: %w", lt.table, err)
		}
	}

	return tx.Commit()
}
