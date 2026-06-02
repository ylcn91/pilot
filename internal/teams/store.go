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

// migrate creates necessary tables for team management
func (s *Store) migrate() error {
	migrations := []string{
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
		// GH-634: Add github_user column for GitHub identity mapping
		`ALTER TABLE team_members ADD COLUMN github_user TEXT`,
		// GH-634: Add telegram_id column for Telegram identity mapping
		`ALTER TABLE team_members ADD COLUMN telegram_id INTEGER DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id)`,
		`CREATE INDEX IF NOT EXISTS idx_team_members_email ON team_members(email)`,
		`CREATE INDEX IF NOT EXISTS idx_team_members_github_user ON team_members(github_user)`,
		`CREATE INDEX IF NOT EXISTS idx_team_members_telegram_id ON team_members(telegram_id)`,
		// GH-783: Add slack_user_id column for Slack identity mapping
		`ALTER TABLE team_members ADD COLUMN slack_user_id TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_team_members_slack_user_id ON team_members(slack_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_team_audit_log_team ON team_audit_log(team_id)`,
		`CREATE INDEX IF NOT EXISTS idx_team_audit_log_created ON team_audit_log(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_team_audit_log_actor ON team_audit_log(actor_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_access_project ON project_access(project_path)`,
	}

	for _, migration := range migrations {
		_, err := s.db.Exec(migration)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "duplicate column") {
				continue
			}
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}
