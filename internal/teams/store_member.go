package teams

import (
	"database/sql"
	"encoding/json"
)

// AddMember adds a member to a team
func (s *Store) AddMember(member *Member) error {
	projects, _ := json.Marshal(member.Projects)
	_, err := s.db.Exec(`
		INSERT INTO team_members (id, team_id, email, name, github_user, telegram_id, slack_user_id, role, projects, joined_at, invited_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, member.ID, member.TeamID, member.Email, member.Name, member.GitHubUser, member.TelegramID, member.SlackUserID, string(member.Role), string(projects), member.JoinedAt, member.InvitedBy)
	return err
}

// memberColumns is the standard set of columns selected from team_members.
const memberColumns = `id, team_id, email, name, github_user, telegram_id, slack_user_id, role, projects, joined_at, invited_by`

// GetMember retrieves a member by ID
func (s *Store) GetMember(id string) (*Member, error) {
	row := s.db.QueryRow(`SELECT `+memberColumns+` FROM team_members WHERE id = ?`, id)
	return s.scanMember(row)
}

// GetMemberByEmail retrieves a member by email within a team
func (s *Store) GetMemberByEmail(teamID, email string) (*Member, error) {
	row := s.db.QueryRow(`SELECT `+memberColumns+` FROM team_members WHERE team_id = ? AND email = ?`, teamID, email)
	return s.scanMember(row)
}

// GetMemberByGitHubUser retrieves a member by GitHub username within a team (GH-634).
func (s *Store) GetMemberByGitHubUser(teamID, ghUser string) (*Member, error) {
	row := s.db.QueryRow(`SELECT `+memberColumns+` FROM team_members WHERE team_id = ? AND github_user = ?`, teamID, ghUser)
	return s.scanMember(row)
}

// GetMembersByGitHubUser retrieves all memberships for a GitHub username (across teams) (GH-634).
func (s *Store) GetMembersByGitHubUser(ghUser string) ([]*Member, error) {
	rows, err := s.db.Query(`SELECT `+memberColumns+` FROM team_members WHERE github_user = ?`, ghUser)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanMembers(rows)
}

// GetMemberByTelegramID retrieves a member by Telegram user ID within a team (GH-634).
func (s *Store) GetMemberByTelegramID(teamID string, telegramID int64) (*Member, error) {
	row := s.db.QueryRow(`SELECT `+memberColumns+` FROM team_members WHERE team_id = ? AND telegram_id = ?`, teamID, telegramID)
	return s.scanMember(row)
}

// GetMembersByTelegramID retrieves all memberships for a Telegram user ID (across teams) (GH-634).
func (s *Store) GetMembersByTelegramID(telegramID int64) ([]*Member, error) {
	rows, err := s.db.Query(`SELECT `+memberColumns+` FROM team_members WHERE telegram_id = ? AND telegram_id != 0`, telegramID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanMembers(rows)
}

// GetMembersBySlackUserID retrieves all memberships for a Slack user ID (across teams) (GH-783).
func (s *Store) GetMembersBySlackUserID(slackUserID string) ([]*Member, error) {
	rows, err := s.db.Query(`SELECT `+memberColumns+` FROM team_members WHERE slack_user_id = ? AND slack_user_id != ''`, slackUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanMembers(rows)
}

// GetMembersByEmail retrieves all memberships for an email (across teams)
func (s *Store) GetMembersByEmail(email string) ([]*Member, error) {
	rows, err := s.db.Query(`SELECT `+memberColumns+` FROM team_members WHERE email = ?`, email)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanMembers(rows)
}

// ListMembers retrieves all members of a team
func (s *Store) ListMembers(teamID string) ([]*Member, error) {
	rows, err := s.db.Query(`SELECT `+memberColumns+` FROM team_members WHERE team_id = ? ORDER BY role, email`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanMembers(rows)
}

// UpdateMember updates a member
func (s *Store) UpdateMember(member *Member) error {
	projects, _ := json.Marshal(member.Projects)
	_, err := s.db.Exec(`
		UPDATE team_members SET name = ?, github_user = ?, telegram_id = ?, slack_user_id = ?, role = ?, projects = ?
		WHERE id = ?
	`, member.Name, member.GitHubUser, member.TelegramID, member.SlackUserID, string(member.Role), string(projects), member.ID)
	return err
}

// RemoveMember removes a member from a team
func (s *Store) RemoveMember(id string) error {
	_, err := s.db.Exec(`DELETE FROM team_members WHERE id = ?`, id)
	return err
}

// CountMembersByRole counts members by role in a team
func (s *Store) CountMembersByRole(teamID string, role Role) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM team_members WHERE team_id = ? AND role = ?
	`, teamID, string(role)).Scan(&count)
	return count, err
}

// scanMember scans a single member row
func (s *Store) scanMember(row *sql.Row) (*Member, error) {
	var member Member
	var name, ghUser, slackUserID, projects, invitedBy sql.NullString
	var telegramID sql.NullInt64
	var roleStr string

	if err := row.Scan(&member.ID, &member.TeamID, &member.Email, &name, &ghUser, &telegramID, &slackUserID, &roleStr, &projects, &member.JoinedAt, &invitedBy); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	member.Role = Role(roleStr)
	if name.Valid {
		member.Name = name.String
	}
	if ghUser.Valid {
		member.GitHubUser = ghUser.String
	}
	if telegramID.Valid {
		member.TelegramID = telegramID.Int64
	}
	if slackUserID.Valid {
		member.SlackUserID = slackUserID.String
	}
	if invitedBy.Valid {
		member.InvitedBy = invitedBy.String
	}
	if projects.Valid && projects.String != "" {
		_ = json.Unmarshal([]byte(projects.String), &member.Projects)
	}

	return &member, nil
}

// scanMembers scans multiple member rows
func (s *Store) scanMembers(rows *sql.Rows) ([]*Member, error) {
	var members []*Member
	for rows.Next() {
		var member Member
		var name, ghUser, slackUserID, projects, invitedBy sql.NullString
		var telegramID sql.NullInt64
		var roleStr string

		if err := rows.Scan(&member.ID, &member.TeamID, &member.Email, &name, &ghUser, &telegramID, &slackUserID, &roleStr, &projects, &member.JoinedAt, &invitedBy); err != nil {
			return nil, err
		}

		member.Role = Role(roleStr)
		if name.Valid {
			member.Name = name.String
		}
		if ghUser.Valid {
			member.GitHubUser = ghUser.String
		}
		if telegramID.Valid {
			member.TelegramID = telegramID.Int64
		}
		if slackUserID.Valid {
			member.SlackUserID = slackUserID.String
		}
		if invitedBy.Valid {
			member.InvitedBy = invitedBy.String
		}
		if projects.Valid && projects.String != "" {
			_ = json.Unmarshal([]byte(projects.String), &member.Projects)
		}

		members = append(members, &member)
	}
	return members, nil
}
