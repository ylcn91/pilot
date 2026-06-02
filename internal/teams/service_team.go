package teams

import (
	"fmt"
)

// CreateTeam creates a new team with the given owner
func (s *Service) CreateTeam(name, ownerEmail string) (*Team, *Member, error) {
	team, owner := NewTeam(name, ownerEmail)

	if err := s.store.CreateTeam(team); err != nil {
		return nil, nil, fmt.Errorf("failed to create team: %w", err)
	}

	if err := s.store.AddMember(owner); err != nil {
		// Rollback team creation
		_ = s.store.DeleteTeam(team.ID)
		return nil, nil, fmt.Errorf("failed to add owner: %w", err)
	}

	// Log team creation
	_ = s.logAudit(team.ID, owner.ID, owner.Email, AuditTeamCreated, "team", team.ID, map[string]interface{}{
		"name": team.Name,
	})

	return team, owner, nil
}

// GetTeam retrieves a team by ID
func (s *Service) GetTeam(teamID string) (*Team, error) {
	team, err := s.store.GetTeam(teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}
	return team, nil
}

// GetTeamByName retrieves a team by name
func (s *Service) GetTeamByName(name string) (*Team, error) {
	return s.store.GetTeamByName(name)
}

// ListTeams retrieves all teams
func (s *Service) ListTeams() ([]*Team, error) {
	return s.store.ListTeams()
}

// UpdateTeamSettings updates team settings
func (s *Service) UpdateTeamSettings(teamID, actorID string, settings Settings) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageTeam) {
		return ErrPermissionDenied
	}

	team, err := s.store.GetTeam(teamID)
	if err != nil || team == nil {
		return ErrTeamNotFound
	}

	team.Settings = settings
	if err := s.store.UpdateTeam(team); err != nil {
		return fmt.Errorf("failed to update team: %w", err)
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditSettingsChanged, "team", teamID, map[string]interface{}{
		"settings": settings,
	})

	return nil
}

// DeleteTeam deletes a team (only owner can do this)
func (s *Service) DeleteTeam(teamID, actorID string) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if actor.Role != RoleOwner {
		return ErrPermissionDenied
	}

	team, err := s.store.GetTeam(teamID)
	if err != nil || team == nil {
		return ErrTeamNotFound
	}

	// Log before deletion
	_ = s.logAudit(teamID, actorID, actor.Email, AuditTeamDeleted, "team", teamID, map[string]interface{}{
		"name": team.Name,
	})

	return s.store.DeleteTeam(teamID)
}
