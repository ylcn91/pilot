package teams

// ServiceAdapter wraps teams.Service to satisfy executor.TeamChecker interface (GH-634).
// It converts string-typed permissions to teams.Permission, decoupling the executor
// package from direct dependency on the teams package.
type ServiceAdapter struct {
	service *Service
}

// NewServiceAdapter creates a ServiceAdapter wrapping the given teams service.
func NewServiceAdapter(service *Service) *ServiceAdapter {
	return &ServiceAdapter{service: service}
}

// CheckPermission verifies a member has a specific permission.
// The perm string is cast to teams.Permission (e.g., "execute_tasks" → PermExecuteTasks).
func (a *ServiceAdapter) CheckPermission(memberID string, perm string) error {
	return a.service.CheckPermission(memberID, Permission(perm))
}

// CheckProjectAccess verifies a member can perform an action on a specific project.
// Checks both the base permission and project-level access restrictions.
func (a *ServiceAdapter) CheckProjectAccess(memberID, projectPath string, requiredPerm string) error {
	return a.service.CheckProjectAccess(memberID, projectPath, Permission(requiredPerm))
}

// ResolveGitHubIdentity resolves a GitHub username and/or email to a team member ID (GH-634).
// Returns ("", nil) when no matching member is found.
func (a *ServiceAdapter) ResolveGitHubIdentity(ghUser, email string) (string, error) {
	return a.service.ResolveGitHubIdentity(ghUser, email)
}

// ResolveTelegramIdentity resolves a Telegram user ID and/or email to a team member ID (GH-634).
// Returns ("", nil) when no matching member is found.
func (a *ServiceAdapter) ResolveTelegramIdentity(telegramID int64, email string) (string, error) {
	return a.service.ResolveTelegramIdentity(telegramID, email)
}

// ResolveSlackIdentity resolves a Slack user ID and/or email to a team member ID (GH-784).
// Returns ("", nil) when no matching member is found.
func (a *ServiceAdapter) ResolveSlackIdentity(slackUserID, email string) (string, error) {
	return a.service.ResolveSlackIdentity(slackUserID, email)
}

// LogTaskEvent records a task-lifecycle audit entry for the resolved member
// (#35). It looks up the member's team and email so callers only need the
// memberID returned by the Resolve* methods. No-op (nil) when memberID is empty
// or the member can't be found, so unattributed daemon tasks don't error.
func (a *ServiceAdapter) LogTaskEvent(memberID, taskID string, action AuditAction, details map[string]interface{}) error {
	if a == nil || a.service == nil || memberID == "" {
		return nil
	}
	member, err := a.service.GetMember(memberID)
	if err != nil || member == nil {
		return nil
	}
	return a.service.LogTaskEvent(member.TeamID, member.ID, member.Email, taskID, action, details)
}
