package teams

import (
	"database/sql"
	"encoding/json"
	"time"
)

// CreateTeam creates a new team
func (s *Store) CreateTeam(team *Team) error {
	settings, _ := json.Marshal(team.Settings)
	_, err := s.db.Exec(`
		INSERT INTO teams (id, name, settings, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, team.ID, team.Name, string(settings), team.CreatedAt, team.UpdatedAt)
	return err
}

// GetTeam retrieves a team by ID
func (s *Store) GetTeam(id string) (*Team, error) {
	row := s.db.QueryRow(`
		SELECT id, name, settings, created_at, updated_at
		FROM teams WHERE id = ?
	`, id)

	var team Team
	var settingsStr sql.NullString
	if err := row.Scan(&team.ID, &team.Name, &settingsStr, &team.CreatedAt, &team.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if settingsStr.Valid && settingsStr.String != "" {
		_ = json.Unmarshal([]byte(settingsStr.String), &team.Settings)
	}

	return &team, nil
}

// GetTeamByName retrieves a team by name
func (s *Store) GetTeamByName(name string) (*Team, error) {
	row := s.db.QueryRow(`
		SELECT id, name, settings, created_at, updated_at
		FROM teams WHERE name = ?
	`, name)

	var team Team
	var settingsStr sql.NullString
	if err := row.Scan(&team.ID, &team.Name, &settingsStr, &team.CreatedAt, &team.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if settingsStr.Valid && settingsStr.String != "" {
		_ = json.Unmarshal([]byte(settingsStr.String), &team.Settings)
	}

	return &team, nil
}

// UpdateTeam updates a team
func (s *Store) UpdateTeam(team *Team) error {
	settings, _ := json.Marshal(team.Settings)
	team.UpdatedAt = time.Now()
	_, err := s.db.Exec(`
		UPDATE teams SET name = ?, settings = ?, updated_at = ?
		WHERE id = ?
	`, team.Name, string(settings), team.UpdatedAt, team.ID)
	return err
}

// DeleteTeam deletes a team (cascades to members, audit log, project access)
func (s *Store) DeleteTeam(id string) error {
	_, err := s.db.Exec(`DELETE FROM teams WHERE id = ?`, id)
	return err
}

// ListTeams retrieves all teams
func (s *Store) ListTeams() ([]*Team, error) {
	rows, err := s.db.Query(`
		SELECT id, name, settings, created_at, updated_at
		FROM teams ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var teams []*Team
	for rows.Next() {
		var team Team
		var settingsStr sql.NullString
		if err := rows.Scan(&team.ID, &team.Name, &settingsStr, &team.CreatedAt, &team.UpdatedAt); err != nil {
			return nil, err
		}
		if settingsStr.Valid && settingsStr.String != "" {
			_ = json.Unmarshal([]byte(settingsStr.String), &team.Settings)
		}
		teams = append(teams, &team)
	}

	return teams, nil
}
