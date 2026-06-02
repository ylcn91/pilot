package teams

import (
	"fmt"
)

// AddMember adds a new member to a team
func (s *Service) AddMember(teamID, actorID, email string, role Role, projects []string) (*Member, error) {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return nil, ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageMembers) {
		return nil, ErrPermissionDenied
	}

	// Can't add someone with higher role than yourself (except owner)
	if role != RoleOwner && !actor.Role.CanManage(role) && actor.Role != RoleOwner {
		return nil, ErrPermissionDenied
	}

	if !role.IsValid() {
		return nil, ErrInvalidRole
	}

	// Check if already a member
	existing, _ := s.store.GetMemberByEmail(teamID, email)
	if existing != nil {
		return nil, ErrAlreadyMember
	}

	member := NewMember(teamID, email, role, actorID)
	member.Projects = projects

	if err := s.store.AddMember(member); err != nil {
		return nil, fmt.Errorf("failed to add member: %w", err)
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditMemberAdded, "member", member.ID, map[string]interface{}{
		"email":    email,
		"role":     string(role),
		"projects": projects,
	})

	return member, nil
}

// RemoveMember removes a member from a team
func (s *Service) RemoveMember(teamID, actorID, memberID string) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageMembers) {
		return ErrPermissionDenied
	}

	member, err := s.store.GetMember(memberID)
	if err != nil || member == nil {
		return ErrMemberNotFound
	}

	// Can't remove owner unless there's another owner
	if member.Role == RoleOwner {
		count, _ := s.store.CountMembersByRole(teamID, RoleOwner)
		if count <= 1 {
			return ErrLastOwner
		}
	}

	// Can't remove someone with equal or higher role (unless you're owner)
	if actor.Role != RoleOwner && !actor.Role.CanManage(member.Role) {
		return ErrPermissionDenied
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditMemberRemoved, "member", memberID, map[string]interface{}{
		"email": member.Email,
		"role":  string(member.Role),
	})

	return s.store.RemoveMember(memberID)
}

// UpdateMemberRole updates a member's role
func (s *Service) UpdateMemberRole(teamID, actorID, memberID string, newRole Role) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageMembers) {
		return ErrPermissionDenied
	}

	member, err := s.store.GetMember(memberID)
	if err != nil || member == nil {
		return ErrMemberNotFound
	}

	// Can't change own role
	if actorID == memberID {
		return ErrSelfRoleChange
	}

	// Can't demote/promote to equal or higher role than yourself (unless owner)
	if actor.Role != RoleOwner {
		if !actor.Role.CanManage(member.Role) || !actor.Role.CanManage(newRole) {
			return ErrPermissionDenied
		}
	}

	// If demoting from owner, ensure there's another owner
	if member.Role == RoleOwner && newRole != RoleOwner {
		count, _ := s.store.CountMembersByRole(teamID, RoleOwner)
		if count <= 1 {
			return ErrLastOwner
		}
	}

	if !newRole.IsValid() {
		return ErrInvalidRole
	}

	oldRole := member.Role
	member.Role = newRole
	if err := s.store.UpdateMember(member); err != nil {
		return fmt.Errorf("failed to update member: %w", err)
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditRoleChanged, "member", memberID, map[string]interface{}{
		"email":    member.Email,
		"old_role": string(oldRole),
		"new_role": string(newRole),
	})

	return nil
}

// UpdateMemberProjects updates a member's project access
func (s *Service) UpdateMemberProjects(teamID, actorID, memberID string, projects []string) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageMembers) {
		return ErrPermissionDenied
	}

	member, err := s.store.GetMember(memberID)
	if err != nil || member == nil {
		return ErrMemberNotFound
	}

	member.Projects = projects
	if err := s.store.UpdateMember(member); err != nil {
		return fmt.Errorf("failed to update member: %w", err)
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditMemberUpdated, "member", memberID, map[string]interface{}{
		"email":    member.Email,
		"projects": projects,
	})

	return nil
}

// GetMember retrieves a member by ID
func (s *Service) GetMember(memberID string) (*Member, error) {
	return s.store.GetMember(memberID)
}

// GetMemberByEmail retrieves a member by email in a team
func (s *Service) GetMemberByEmail(teamID, email string) (*Member, error) {
	return s.store.GetMemberByEmail(teamID, email)
}

// GetMemberByGitHubUser retrieves a member by GitHub username in a team (GH-634).
func (s *Service) GetMemberByGitHubUser(teamID, ghUser string) (*Member, error) {
	return s.store.GetMemberByGitHubUser(teamID, ghUser)
}

// ListMembers lists all members of a team
func (s *Service) ListMembers(teamID string) ([]*Member, error) {
	return s.store.ListMembers(teamID)
}

// GetTeamsForUser retrieves all teams a user belongs to
func (s *Service) GetTeamsForUser(email string) ([]*TeamMembership, error) {
	members, err := s.store.GetMembersByEmail(email)
	if err != nil {
		return nil, err
	}

	var memberships []*TeamMembership
	for _, m := range members {
		team, err := s.store.GetTeam(m.TeamID)
		if err != nil || team == nil {
			continue
		}
		memberships = append(memberships, &TeamMembership{
			Team:   team,
			Member: m,
		})
	}

	return memberships, nil
}

// TeamMembership combines team and member info
type TeamMembership struct {
	Team   *Team
	Member *Member
}
