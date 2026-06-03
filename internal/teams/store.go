package teams

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Store provides persistent storage for teams
type Store struct {
	db *sql.DB
}

// NewStore creates a new team store using an existing database connection
func NewStore(db *sql.DB) (*Store, error) {
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate team tables: %w", err)
	}
	return store, nil
}

// migration is one ordered, version-numbered schema change. Migrations are
// applied in slice order; each is recorded in PRAGMA user_version so a given
// version runs at most once per database. This replaces the previous flat
// statement list that depended on string-matching "duplicate column" errors
// to stay idempotent across re-runs.
type migration struct {
	// version is the user_version the database reaches after this migration
	// commits. Versions must be strictly increasing and start at 1.
	version int
	stmts   []string
}

// schemaMigrations is the ordered set of team-table migrations. Append new
// migrations with the next version number; never renumber or reorder existing
// entries (their version is the on-disk contract).
//
// The grouping into versions preserves the exact statement order the legacy
// flat list applied, so a fresh database ends up with a byte-identical schema.
var schemaMigrations = []migration{
	{
		version: 1,
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS teams (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				settings TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS team_members (
				id TEXT PRIMARY KEY,
				team_id TEXT NOT NULL,
				email TEXT NOT NULL,
				name TEXT,
				role TEXT NOT NULL,
				projects TEXT,
				joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				invited_by TEXT,
				FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
				UNIQUE(team_id, email)
			)`,
			`CREATE TABLE IF NOT EXISTS team_audit_log (
				id TEXT PRIMARY KEY,
				team_id TEXT NOT NULL,
				actor_id TEXT NOT NULL,
				actor_email TEXT NOT NULL,
				action TEXT NOT NULL,
				resource TEXT NOT NULL,
				resource_id TEXT,
				details TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS project_access (
				team_id TEXT NOT NULL,
				project_path TEXT NOT NULL,
				default_role TEXT NOT NULL DEFAULT 'developer',
				PRIMARY KEY (team_id, project_path),
				FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
			)`,
		},
	},
	{
		// GH-634: GitHub + Telegram identity mapping columns and their indexes.
		version: 2,
		stmts: []string{
			`ALTER TABLE team_members ADD COLUMN github_user TEXT`,
			`ALTER TABLE team_members ADD COLUMN telegram_id INTEGER DEFAULT 0`,
			`CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id)`,
			`CREATE INDEX IF NOT EXISTS idx_team_members_email ON team_members(email)`,
			`CREATE INDEX IF NOT EXISTS idx_team_members_github_user ON team_members(github_user)`,
			`CREATE INDEX IF NOT EXISTS idx_team_members_telegram_id ON team_members(telegram_id)`,
		},
	},
	{
		// GH-783: Slack identity mapping column and index.
		version: 3,
		stmts: []string{
			`ALTER TABLE team_members ADD COLUMN slack_user_id TEXT`,
			`CREATE INDEX IF NOT EXISTS idx_team_members_slack_user_id ON team_members(slack_user_id)`,
		},
	},
	{
		version: 4,
		stmts: []string{
			`CREATE INDEX IF NOT EXISTS idx_team_audit_log_team ON team_audit_log(team_id)`,
			`CREATE INDEX IF NOT EXISTS idx_team_audit_log_created ON team_audit_log(created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_team_audit_log_actor ON team_audit_log(actor_id)`,
			`CREATE INDEX IF NOT EXISTS idx_project_access_project ON project_access(project_path)`,
		},
	},
}

// migrate brings the database schema up to the latest version. It reads the
// current schema version from PRAGMA user_version, applies every migration
// with a higher version in order, and advances user_version after each. Running
// migrate twice (e.g. a second NewStore on the same DB) is a no-op because the
// recorded version already covers every migration.
func (s *Store) migrate() error {
	var current int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range schemaMigrations {
		if m.version <= current {
			continue
		}
		if err := s.applyMigration(m); err != nil {
			return err
		}
		current = m.version
	}

	return nil
}

// applyMigration runs every statement in a migration and then records its
// version via PRAGMA user_version. PRAGMA user_version cannot be parameterized,
// so the (trusted, integer) version is interpolated directly.
//
// ALTER TABLE ADD COLUMN statements are made idempotent against legacy
// databases that pre-date the version table: such databases report
// user_version 0 yet may already carry the columns, so a re-run would hit
// "duplicate column". Those are tolerated; any other error aborts the
// migration so the version is not advanced past a failed step.
func (s *Store) applyMigration(m migration) error {
	for _, stmt := range m.stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			if isDuplicateColumn(err) {
				continue
			}
			return fmt.Errorf("migration v%d failed: %w", m.version, err)
		}
	}

	if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
		return fmt.Errorf("set schema version %d: %w", m.version, err)
	}

	return nil
}

// isDuplicateColumn reports whether err is SQLite's "duplicate column" error,
// raised when an ALTER TABLE ADD COLUMN targets a column that already exists.
func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}
