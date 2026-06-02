package memory

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"
)

// Pattern represents a learned pattern from project executions.
// Patterns capture recurring code structures, workflows, or solutions
// that can be applied to future similar tasks.
type Pattern struct {
	ID          int64
	ProjectPath string
	Type        string
	Content     string
	Confidence  float64
	Uses        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SavePattern saves a new pattern or updates an existing one.
// If pattern.ID is zero, a new pattern is inserted; otherwise the existing pattern is updated.
func (s *Store) SavePattern(pattern *Pattern) error {
	if pattern.ID == 0 {
		return s.withRetry("SavePattern", func() error {
			result, err := s.db.Exec(`
				INSERT INTO patterns (project_path, pattern_type, content, confidence)
				VALUES (?, ?, ?, ?)
			`, pattern.ProjectPath, pattern.Type, pattern.Content, pattern.Confidence)
			if err != nil {
				return err
			}
			id, _ := result.LastInsertId()
			pattern.ID = id
			return nil
		})
	}
	return s.withRetry("SavePattern", func() error {
		_, err := s.db.Exec(`
			UPDATE patterns SET content = ?, confidence = ?, uses = uses + 1, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, pattern.Content, pattern.Confidence, pattern.ID)
		return err
	})
}

// GetPatterns retrieves patterns applicable to a project.
// Returns both project-specific patterns and global patterns (those with no project path).
// Results are ordered by confidence and usage count descending.
func (s *Store) GetPatterns(projectPath string) ([]*Pattern, error) {
	rows, err := s.db.Query(`
		SELECT id, project_path, pattern_type, content, confidence, uses, created_at, updated_at
		FROM patterns WHERE project_path = ? OR project_path IS NULL
		ORDER BY confidence DESC, uses DESC
	`, projectPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var patterns []*Pattern
	for rows.Next() {
		var p Pattern
		var projectPath sql.NullString
		if err := rows.Scan(&p.ID, &projectPath, &p.Type, &p.Content, &p.Confidence, &p.Uses, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if projectPath.Valid {
			p.ProjectPath = projectPath.String
		}
		patterns = append(patterns, &p)
	}

	return patterns, rows.Err()
}

// Project represents a registered project in Pilot.
// It stores project metadata, Navigator settings, and custom configuration.
type Project struct {
	Path             string
	Name             string
	NavigatorEnabled bool
	LastActive       time.Time
	Settings         map[string]interface{}
}

// SaveProject saves or updates a project in the database.
// If a project with the same path exists, it is updated; otherwise a new record is created.
func (s *Store) SaveProject(project *Project) error {
	settings, _ := json.Marshal(project.Settings)
	return s.withRetry("SaveProject", func() error {
		_, err := s.db.Exec(`
			INSERT INTO projects (path, name, navigator_enabled, settings)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				name = excluded.name,
				navigator_enabled = excluded.navigator_enabled,
				last_active = CURRENT_TIMESTAMP,
				settings = excluded.settings
		`, project.Path, project.Name, project.NavigatorEnabled, string(settings))
		return err
	})
}

// GetProject retrieves a project by its filesystem path.
// Returns sql.ErrNoRows if the project is not found.
func (s *Store) GetProject(path string) (*Project, error) {
	row := s.db.QueryRow(`
		SELECT path, name, navigator_enabled, last_active, settings
		FROM projects WHERE path = ?
	`, path)

	var p Project
	var settingsStr string
	if err := row.Scan(&p.Path, &p.Name, &p.NavigatorEnabled, &p.LastActive, &settingsStr); err != nil {
		return nil, err
	}

	if settingsStr != "" {
		if err := json.Unmarshal([]byte(settingsStr), &p.Settings); err != nil {
			slog.Warn("failed to unmarshal project settings",
				slog.String("project_path", p.Path),
				slog.Any("error", err))
		}
	}

	return &p, nil
}

// GetAllProjects retrieves all registered projects ordered by last activity time.
func (s *Store) GetAllProjects() ([]*Project, error) {
	rows, err := s.db.Query(`
		SELECT path, name, navigator_enabled, last_active, settings
		FROM projects ORDER BY last_active DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []*Project
	for rows.Next() {
		var p Project
		var settingsStr string
		if err := rows.Scan(&p.Path, &p.Name, &p.NavigatorEnabled, &p.LastActive, &settingsStr); err != nil {
			return nil, err
		}
		if settingsStr != "" {
			if err := json.Unmarshal([]byte(settingsStr), &p.Settings); err != nil {
				slog.Warn("failed to unmarshal project settings",
					slog.String("project_path", p.Path),
					slog.Any("error", err))
			}
		}
		projects = append(projects, &p)
	}

	return projects, rows.Err()
}
