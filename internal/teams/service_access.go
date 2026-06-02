package teams

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CheckPermission checks if a member has a specific permission
func (s *Service) CheckPermission(memberID string, perm Permission) error {
	member, err := s.store.GetMember(memberID)
	if err != nil || member == nil {
		return ErrMemberNotFound
	}

	if !member.Role.HasPermission(perm) {
		return ErrPermissionDenied
	}

	return nil
}

// CheckProjectAccess checks if a member can access a project
func (s *Service) CheckProjectAccess(memberID, projectPath string, requiredPerm Permission) error {
	member, err := s.store.GetMember(memberID)
	if err != nil || member == nil {
		return ErrMemberNotFound
	}

	// Check base permission
	if !member.Role.HasPermission(requiredPerm) {
		return ErrPermissionDenied
	}

	// If member has restricted project access, check it
	if len(member.Projects) > 0 {
		allowed := false
		for _, p := range member.Projects {
			if p == projectPath {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrPermissionDenied
		}
	}

	return nil
}

// SetProjectAccess sets the default role for a project
func (s *Service) SetProjectAccess(teamID, actorID, projectPath string, defaultRole Role) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageProjects) {
		return ErrPermissionDenied
	}

	access := &ProjectAccess{
		TeamID:      teamID,
		ProjectPath: projectPath,
		DefaultRole: defaultRole,
	}

	if err := s.store.SetProjectAccess(access); err != nil {
		return fmt.Errorf("failed to set project access: %w", err)
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditProjectAdded, "project", projectPath, map[string]interface{}{
		"default_role": string(defaultRole),
	})

	return nil
}

// RemoveProjectAccess removes project access from a team
func (s *Service) RemoveProjectAccess(teamID, actorID, projectPath string) error {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermManageProjects) {
		return ErrPermissionDenied
	}

	_ = s.logAudit(teamID, actorID, actor.Email, AuditProjectRemoved, "project", projectPath, nil)

	return s.store.RemoveProjectAccess(teamID, projectPath)
}

// ListProjectAccess lists all project access entries for a team
func (s *Service) ListProjectAccess(teamID string) ([]*ProjectAccess, error) {
	return s.store.ListProjectAccess(teamID)
}

// GetAuditLog retrieves audit log entries
func (s *Service) GetAuditLog(teamID, actorID string, limit int) ([]*AuditEntry, error) {
	actor, err := s.store.GetMember(actorID)
	if err != nil || actor == nil {
		return nil, ErrMemberNotFound
	}

	if !actor.Role.HasPermission(PermViewAuditLog) {
		return nil, ErrPermissionDenied
	}

	return s.store.GetAuditLog(teamID, limit)
}

// LogTaskEvent logs a task-related audit event
func (s *Service) LogTaskEvent(teamID, actorID, actorEmail, taskID string, action AuditAction, details map[string]interface{}) error {
	return s.logAudit(teamID, actorID, actorEmail, action, "task", taskID, details)
}

// logAudit creates an audit log entry
func (s *Service) logAudit(teamID, actorID, actorEmail string, action AuditAction, resource, resourceID string, details map[string]interface{}) error {
	entry := &AuditEntry{
		ID:         uuid.New().String(),
		TeamID:     teamID,
		ActorID:    actorID,
		ActorEmail: actorEmail,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
		CreatedAt:  time.Now(),
	}
	return s.store.AddAuditEntry(entry)
}
