package teams

import (
	"database/sql"
	"encoding/json"
)

// AddAuditEntry adds an audit log entry
func (s *Store) AddAuditEntry(entry *AuditEntry) error {
	details, _ := json.Marshal(entry.Details)
	_, err := s.db.Exec(`
		INSERT INTO team_audit_log (id, team_id, actor_id, actor_email, action, resource, resource_id, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.TeamID, entry.ActorID, entry.ActorEmail, string(entry.Action), entry.Resource, entry.ResourceID, string(details), entry.CreatedAt)
	return err
}

// GetAuditLog retrieves audit log entries for a team
func (s *Store) GetAuditLog(teamID string, limit int) ([]*AuditEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, team_id, actor_id, actor_email, action, resource, resource_id, details, created_at
		FROM team_audit_log WHERE team_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, teamID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []*AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var resourceID, detailsStr sql.NullString
		var actionStr string

		if err := rows.Scan(&entry.ID, &entry.TeamID, &entry.ActorID, &entry.ActorEmail, &actionStr, &entry.Resource, &resourceID, &detailsStr, &entry.CreatedAt); err != nil {
			return nil, err
		}

		entry.Action = AuditAction(actionStr)
		if resourceID.Valid {
			entry.ResourceID = resourceID.String
		}
		if detailsStr.Valid && detailsStr.String != "" {
			_ = json.Unmarshal([]byte(detailsStr.String), &entry.Details)
		}

		entries = append(entries, &entry)
	}

	return entries, nil
}

// SetProjectAccess sets the default role for a project within a team
func (s *Store) SetProjectAccess(access *ProjectAccess) error {
	_, err := s.db.Exec(`
		INSERT INTO project_access (team_id, project_path, default_role)
		VALUES (?, ?, ?)
		ON CONFLICT(team_id, project_path) DO UPDATE SET
			default_role = excluded.default_role
	`, access.TeamID, access.ProjectPath, string(access.DefaultRole))
	return err
}

// GetProjectAccess retrieves project access for a team
func (s *Store) GetProjectAccess(teamID, projectPath string) (*ProjectAccess, error) {
	row := s.db.QueryRow(`
		SELECT team_id, project_path, default_role
		FROM project_access WHERE team_id = ? AND project_path = ?
	`, teamID, projectPath)

	var access ProjectAccess
	var roleStr string
	if err := row.Scan(&access.TeamID, &access.ProjectPath, &roleStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	access.DefaultRole = Role(roleStr)

	return &access, nil
}

// ListProjectAccess retrieves all project access entries for a team
func (s *Store) ListProjectAccess(teamID string) ([]*ProjectAccess, error) {
	rows, err := s.db.Query(`
		SELECT team_id, project_path, default_role
		FROM project_access WHERE team_id = ?
		ORDER BY project_path
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var accesses []*ProjectAccess
	for rows.Next() {
		var access ProjectAccess
		var roleStr string
		if err := rows.Scan(&access.TeamID, &access.ProjectPath, &roleStr); err != nil {
			return nil, err
		}
		access.DefaultRole = Role(roleStr)
		accesses = append(accesses, &access)
	}

	return accesses, nil
}

// RemoveProjectAccess removes project access entry
func (s *Store) RemoveProjectAccess(teamID, projectPath string) error {
	_, err := s.db.Exec(`DELETE FROM project_access WHERE team_id = ? AND project_path = ?`, teamID, projectPath)
	return err
}
