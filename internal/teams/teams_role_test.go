package teams

import "testing"

func TestRolePermissions(t *testing.T) {
	tests := []struct {
		role     Role
		perm     Permission
		expected bool
	}{
		// Owner has all permissions
		{RoleOwner, PermManageTeam, true},
		{RoleOwner, PermManageMembers, true},
		{RoleOwner, PermExecuteTasks, true},
		{RoleOwner, PermViewTasks, true},

		// Admin has most permissions but not manage_team
		{RoleAdmin, PermManageTeam, false},
		{RoleAdmin, PermManageMembers, true},
		{RoleAdmin, PermExecuteTasks, true},
		{RoleAdmin, PermViewAuditLog, true},

		// Developer can execute tasks
		{RoleDeveloper, PermManageTeam, false},
		{RoleDeveloper, PermManageMembers, false},
		{RoleDeveloper, PermExecuteTasks, true},
		{RoleDeveloper, PermCreateTasks, true},
		{RoleDeveloper, PermViewTasks, true},

		// Viewer is read-only
		{RoleViewer, PermManageTeam, false},
		{RoleViewer, PermExecuteTasks, false},
		{RoleViewer, PermViewTasks, true},
		{RoleViewer, PermViewProjects, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.role)+"/"+string(tt.perm), func(t *testing.T) {
			got := tt.role.HasPermission(tt.perm)
			if got != tt.expected {
				t.Errorf("Role(%s).HasPermission(%s) = %v, want %v", tt.role, tt.perm, got, tt.expected)
			}
		})
	}
}

func TestRoleCanManage(t *testing.T) {
	tests := []struct {
		role     Role
		other    Role
		expected bool
	}{
		{RoleOwner, RoleAdmin, true},
		{RoleOwner, RoleDeveloper, true},
		{RoleOwner, RoleViewer, true},
		{RoleOwner, RoleOwner, false}, // Can't manage equal role

		{RoleAdmin, RoleDeveloper, true},
		{RoleAdmin, RoleViewer, true},
		{RoleAdmin, RoleAdmin, false},
		{RoleAdmin, RoleOwner, false},

		{RoleDeveloper, RoleViewer, true},
		{RoleDeveloper, RoleDeveloper, false},
		{RoleDeveloper, RoleAdmin, false},

		{RoleViewer, RoleViewer, false},
		{RoleViewer, RoleDeveloper, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role)+"/"+string(tt.other), func(t *testing.T) {
			got := tt.role.CanManage(tt.other)
			if got != tt.expected {
				t.Errorf("Role(%s).CanManage(%s) = %v, want %v", tt.role, tt.other, got, tt.expected)
			}
		})
	}
}

func TestRoleIsValid(t *testing.T) {
	tests := []struct {
		role     Role
		expected bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleDeveloper, true},
		{RoleViewer, true},
		{Role("invalid"), false},
		{Role(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got := tt.role.IsValid()
			if got != tt.expected {
				t.Errorf("Role(%s).IsValid() = %v, want %v", tt.role, got, tt.expected)
			}
		})
	}
}

func TestRoleLevel(t *testing.T) {
	tests := []struct {
		name     string
		role     Role
		expected int
	}{
		{"owner level", RoleOwner, 4},
		{"admin level", RoleAdmin, 3},
		{"developer level", RoleDeveloper, 2},
		{"viewer level", RoleViewer, 1},
		{"invalid role level", Role("invalid"), 0},
		{"empty role level", Role(""), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.role.Level()
			if got != tt.expected {
				t.Errorf("Role(%q).Level() = %d, want %d", tt.role, got, tt.expected)
			}
		})
	}
}

func TestRolePermissions_AllRoles(t *testing.T) {
	tests := []struct {
		name             string
		role             Role
		expectedPermLen  int
		mustHavePerms    []Permission
		mustNotHavePerms []Permission
	}{
		{
			name:            "owner has all permissions",
			role:            RoleOwner,
			expectedPermLen: 10,
			mustHavePerms: []Permission{
				PermManageTeam, PermManageMembers, PermManageBilling,
				PermManageProjects, PermExecuteTasks, PermViewProjects,
				PermCreateTasks, PermCancelTasks, PermViewTasks, PermViewAuditLog,
			},
		},
		{
			name:            "admin permissions",
			role:            RoleAdmin,
			expectedPermLen: 8,
			mustHavePerms: []Permission{
				PermManageMembers, PermManageProjects, PermExecuteTasks,
				PermViewProjects, PermCreateTasks, PermCancelTasks,
				PermViewTasks, PermViewAuditLog,
			},
			mustNotHavePerms: []Permission{PermManageTeam, PermManageBilling},
		},
		{
			name:            "developer permissions",
			role:            RoleDeveloper,
			expectedPermLen: 5,
			mustHavePerms: []Permission{
				PermExecuteTasks, PermViewProjects, PermCreateTasks,
				PermCancelTasks, PermViewTasks,
			},
			mustNotHavePerms: []Permission{
				PermManageTeam, PermManageMembers, PermManageBilling,
				PermManageProjects, PermViewAuditLog,
			},
		},
		{
			name:            "viewer permissions",
			role:            RoleViewer,
			expectedPermLen: 2,
			mustHavePerms:   []Permission{PermViewProjects, PermViewTasks},
			mustNotHavePerms: []Permission{
				PermManageTeam, PermManageMembers, PermManageBilling,
				PermManageProjects, PermExecuteTasks, PermCreateTasks,
				PermCancelTasks, PermViewAuditLog,
			},
		},
		{
			name:            "invalid role has no permissions",
			role:            Role("invalid"),
			expectedPermLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := tt.role.Permissions()
			if len(perms) != tt.expectedPermLen {
				t.Errorf("Role(%q).Permissions() length = %d, want %d", tt.role, len(perms), tt.expectedPermLen)
			}

			for _, perm := range tt.mustHavePerms {
				if !tt.role.HasPermission(perm) {
					t.Errorf("Role(%q) should have permission %q", tt.role, perm)
				}
			}

			for _, perm := range tt.mustNotHavePerms {
				if tt.role.HasPermission(perm) {
					t.Errorf("Role(%q) should not have permission %q", tt.role, perm)
				}
			}
		})
	}
}

func TestHasPermissionInvalidRole(t *testing.T) {
	invalidRole := Role("nonexistent")
	if invalidRole.HasPermission(PermViewTasks) {
		t.Error("invalid role should not have any permissions")
	}
}

// =============================================================================
// Store Tests - Extended Coverage
// =============================================================================
