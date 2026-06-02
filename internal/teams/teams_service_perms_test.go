package teams

import "testing"

func TestService_UpdateMemberRole_SelfChange(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Try to change own role - should fail
	err := service.UpdateMemberRole(team.ID, owner.ID, owner.ID, RoleAdmin)
	if err != ErrSelfRoleChange {
		t.Errorf("expected ErrSelfRoleChange, got %v", err)
	}
}

func TestService_UpdateMemberRole(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	admin, _ := service.AddMember(team.ID, owner.ID, "admin@example.com", RoleAdmin, nil)
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)

	// Test owner promotes dev to admin
	t.Run("owner promotes dev to admin", func(t *testing.T) {
		err := service.UpdateMemberRole(team.ID, owner.ID, dev.ID, RoleAdmin)
		if err != nil {
			t.Errorf("UpdateMemberRole() error = %v, want nil", err)
		}
	})

	// Test owner demotes admin to viewer
	t.Run("owner demotes admin to viewer", func(t *testing.T) {
		err := service.UpdateMemberRole(team.ID, owner.ID, admin.ID, RoleViewer)
		if err != nil {
			t.Errorf("UpdateMemberRole() error = %v, want nil", err)
		}
	})

	// Test self role change - need fresh admin for this
	t.Run("self role change", func(t *testing.T) {
		admin2, _ := service.AddMember(team.ID, owner.ID, "admin2@example.com", RoleAdmin, nil)
		err := service.UpdateMemberRole(team.ID, admin2.ID, admin2.ID, RoleDeveloper)
		if err != ErrSelfRoleChange {
			t.Errorf("UpdateMemberRole() error = %v, want %v", err, ErrSelfRoleChange)
		}
	})

	// Test invalid role
	t.Run("invalid role", func(t *testing.T) {
		dev2, _ := service.AddMember(team.ID, owner.ID, "dev2@example.com", RoleDeveloper, nil)
		err := service.UpdateMemberRole(team.ID, owner.ID, dev2.ID, Role("invalid"))
		if err != ErrInvalidRole {
			t.Errorf("UpdateMemberRole() error = %v, want %v", err, ErrInvalidRole)
		}
	})
}

func TestService_UpdateMemberRole_LastOwnerDemotion(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	admin, _ := service.AddMember(team.ID, owner.ID, "admin@example.com", RoleAdmin, nil)

	// Admin tries to demote the only owner (even if they had permission, this should fail)
	// First, let's make admin an owner so they have permission
	_ = service.UpdateMemberRole(team.ID, owner.ID, admin.ID, RoleOwner)

	// Now there are 2 owners, admin can demote owner
	err := service.UpdateMemberRole(team.ID, admin.ID, owner.ID, RoleAdmin)
	if err != nil {
		t.Errorf("should allow demoting owner when another owner exists: %v", err)
	}

	// Now admin is the only owner, they can't demote themselves
	err = service.UpdateMemberRole(team.ID, admin.ID, admin.ID, RoleDeveloper)
	if err != ErrSelfRoleChange {
		t.Errorf("expected ErrSelfRoleChange, got %v", err)
	}
}

func TestService_UpdateMemberRole_AdminCannotPromoteToAdmin(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	admin, _ := service.AddMember(team.ID, owner.ID, "admin@example.com", RoleAdmin, nil)
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)

	// Admin cannot promote developer to admin (equal role)
	err := service.UpdateMemberRole(team.ID, admin.ID, dev.ID, RoleAdmin)
	if err != ErrPermissionDenied {
		t.Errorf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestService_UpdateMemberProjects(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	tests := []struct {
		name     string
		actorID  string
		memberID string
		projects []string
		wantErr  error
	}{
		{
			name:     "owner updates dev projects",
			actorID:  owner.ID,
			memberID: dev.ID,
			projects: []string{"/project/a", "/project/b"},
			wantErr:  nil,
		},
		{
			name:     "viewer cannot update",
			actorID:  viewer.ID,
			memberID: dev.ID,
			projects: []string{"/project/c"},
			wantErr:  ErrPermissionDenied,
		},
		{
			name:     "nonexistent member",
			actorID:  owner.ID,
			memberID: "nonexistent",
			projects: []string{},
			wantErr:  ErrMemberNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.UpdateMemberProjects(team.ID, tt.actorID, tt.memberID, tt.projects)
			if err != tt.wantErr {
				t.Errorf("UpdateMemberProjects() error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	// Verify projects were updated
	got, _ := service.GetMember(dev.ID)
	if len(got.Projects) != 2 || got.Projects[0] != "/project/a" {
		t.Errorf("projects not updated correctly: %v", got.Projects)
	}
}

func TestService_CheckPermission(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	tests := []struct {
		name     string
		memberID string
		perm     Permission
		wantErr  error
	}{
		{"owner has manage team", owner.ID, PermManageTeam, nil},
		{"owner has view tasks", owner.ID, PermViewTasks, nil},
		{"viewer has view tasks", viewer.ID, PermViewTasks, nil},
		{"viewer lacks execute tasks", viewer.ID, PermExecuteTasks, ErrPermissionDenied},
		{"nonexistent member", "nonexistent", PermViewTasks, ErrMemberNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.CheckPermission(tt.memberID, tt.perm)
			if err != tt.wantErr {
				t.Errorf("CheckPermission() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_CheckProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Add developer with restricted projects
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, []string{"/project/a", "/project/b"})

	// Check allowed project
	err := service.CheckProjectAccess(dev.ID, "/project/a", PermExecuteTasks)
	if err != nil {
		t.Errorf("expected access to /project/a, got error: %v", err)
	}

	// Check disallowed project
	err = service.CheckProjectAccess(dev.ID, "/project/c", PermExecuteTasks)
	if err != ErrPermissionDenied {
		t.Errorf("expected ErrPermissionDenied for /project/c, got %v", err)
	}
}

func TestService_CheckProjectAccess_NoRestrictions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil) // No project restrictions

	// Should have access to any project
	err := service.CheckProjectAccess(dev.ID, "/any/project", PermExecuteTasks)
	if err != nil {
		t.Errorf("expected access to any project, got error: %v", err)
	}
}

func TestService_CheckProjectAccess_InsufficientPermission(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	// Viewer lacks execute permission
	err := service.CheckProjectAccess(viewer.ID, "/project/a", PermExecuteTasks)
	if err != ErrPermissionDenied {
		t.Errorf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestService_SetProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	tests := []struct {
		name        string
		actorID     string
		projectPath string
		defaultRole Role
		wantErr     error
	}{
		{"owner sets project access", owner.ID, "/project/a", RoleDeveloper, nil},
		{"viewer cannot set access", viewer.ID, "/project/b", RoleDeveloper, ErrPermissionDenied},
		{"nonexistent actor", "nonexistent", "/project/c", RoleDeveloper, ErrMemberNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.SetProjectAccess(team.ID, tt.actorID, tt.projectPath, tt.defaultRole)
			if err != tt.wantErr {
				t.Errorf("SetProjectAccess() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_RemoveProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	// Setup: add project access
	_ = service.SetProjectAccess(team.ID, owner.ID, "/project/to-remove", RoleDeveloper)

	tests := []struct {
		name        string
		actorID     string
		projectPath string
		wantErr     error
	}{
		{"viewer cannot remove", viewer.ID, "/project/to-remove", ErrPermissionDenied},
		{"nonexistent actor", "nonexistent", "/project/to-remove", ErrMemberNotFound},
		{"owner removes access", owner.ID, "/project/to-remove", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.RemoveProjectAccess(team.ID, tt.actorID, tt.projectPath)
			if err != tt.wantErr {
				t.Errorf("RemoveProjectAccess() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_ListProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Add project accesses
	_ = service.SetProjectAccess(team.ID, owner.ID, "/project/a", RoleDeveloper)
	_ = service.SetProjectAccess(team.ID, owner.ID, "/project/b", RoleViewer)

	accesses, err := service.ListProjectAccess(team.ID)
	if err != nil {
		t.Fatalf("ListProjectAccess failed: %v", err)
	}

	if len(accesses) != 2 {
		t.Errorf("got %d accesses, want 2", len(accesses))
	}
}
