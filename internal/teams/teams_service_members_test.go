package teams

import "testing"

func TestService_AddMember(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	admin, _ := service.AddMember(team.ID, owner.ID, "admin@example.com", RoleAdmin, nil)

	tests := []struct {
		name     string
		actorID  string
		email    string
		role     Role
		projects []string
		wantErr  error
	}{
		{
			name:    "owner adds developer",
			actorID: owner.ID,
			email:   "dev1@example.com",
			role:    RoleDeveloper,
			wantErr: nil,
		},
		{
			name:    "owner adds admin",
			actorID: owner.ID,
			email:   "admin2@example.com",
			role:    RoleAdmin,
			wantErr: nil,
		},
		{
			name:    "admin adds developer",
			actorID: admin.ID,
			email:   "dev2@example.com",
			role:    RoleDeveloper,
			wantErr: nil,
		},
		{
			name:    "admin cannot add admin",
			actorID: admin.ID,
			email:   "admin3@example.com",
			role:    RoleAdmin,
			wantErr: ErrPermissionDenied,
		},
		{
			name:    "add with invalid role",
			actorID: owner.ID,
			email:   "invalid@example.com",
			role:    Role("invalid"),
			wantErr: ErrInvalidRole,
		},
		{
			name:    "duplicate member",
			actorID: owner.ID,
			email:   "dev1@example.com",
			role:    RoleDeveloper,
			wantErr: ErrAlreadyMember,
		},
		{
			name:     "add with project restrictions",
			actorID:  owner.ID,
			email:    "restricted@example.com",
			role:     RoleDeveloper,
			projects: []string{"/project/a"},
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member, err := service.AddMember(team.ID, tt.actorID, tt.email, tt.role, tt.projects)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("AddMember() error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("AddMember() unexpected error: %v", err)
				}
				if member == nil {
					t.Error("expected member, got nil")
				}
				if member != nil && member.Role != tt.role {
					t.Errorf("got role %s, want %s", member.Role, tt.role)
				}
			}
		})
	}
}

func TestService_AddMember_PermissionDenied(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Add a viewer
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	// Viewer tries to add member - should fail
	_, err := service.AddMember(team.ID, viewer.ID, "new@example.com", RoleDeveloper, nil)
	if err != ErrPermissionDenied {
		t.Errorf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestService_AddMember_ActorNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, _, _ := service.CreateTeam("Test Team", "owner@example.com")

	_, err := service.AddMember(team.ID, "nonexistent", "new@example.com", RoleDeveloper, nil)
	if err != ErrMemberNotFound {
		t.Errorf("expected ErrMemberNotFound, got %v", err)
	}
}

func TestService_RemoveMember(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	admin, _ := service.AddMember(team.ID, owner.ID, "admin@example.com", RoleAdmin, nil)
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	tests := []struct {
		name     string
		actorID  string
		memberID string
		wantErr  error
	}{
		{"viewer cannot remove", viewer.ID, dev.ID, ErrPermissionDenied},
		{"admin removes viewer", admin.ID, viewer.ID, nil},
		{"admin cannot remove admin", admin.ID, admin.ID, ErrPermissionDenied}, // Can't remove self (equal role)
		{"owner removes developer", owner.ID, dev.ID, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.RemoveMember(team.ID, tt.actorID, tt.memberID)
			if err != tt.wantErr {
				t.Errorf("RemoveMember() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestService_RemoveMember_LastOwner(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Try to remove the only owner - should fail
	err := service.RemoveMember(team.ID, owner.ID, owner.ID)
	if err != ErrLastOwner {
		t.Errorf("expected ErrLastOwner, got %v", err)
	}
}

func TestService_RemoveMember_WithMultipleOwners(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner1, _ := service.CreateTeam("Test Team", "owner1@example.com")

	// Add second owner
	owner2, _ := service.AddMember(team.ID, owner1.ID, "owner2@example.com", RoleOwner, nil)

	// Now owner1 can remove owner2
	err := service.RemoveMember(team.ID, owner1.ID, owner2.ID)
	if err != nil {
		t.Errorf("RemoveMember() with multiple owners should succeed: %v", err)
	}
}

func TestService_GetMember(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	got, err := service.GetMember(owner.ID)
	if err != nil {
		t.Fatalf("GetMember failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected member, got nil")
	}
	if got.Email != "owner@example.com" {
		t.Errorf("got email %q, want %q", got.Email, "owner@example.com")
	}

	// Nonexistent
	notFound, err := service.GetMember("nonexistent")
	if err != nil {
		t.Fatalf("GetMember should not error: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for nonexistent member")
	}
	_ = team // used for setup
}

func TestService_GetMemberByEmail(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, _, _ := service.CreateTeam("Test Team", "owner@example.com")

	got, err := service.GetMemberByEmail(team.ID, "owner@example.com")
	if err != nil {
		t.Fatalf("GetMemberByEmail failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected member, got nil")
	}

	// Nonexistent
	notFound, err := service.GetMemberByEmail(team.ID, "nonexistent@example.com")
	if err != nil {
		t.Fatalf("GetMemberByEmail should not error: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for nonexistent email")
	}
}

func TestService_ListMembers(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	_, _ = service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)

	members, err := service.ListMembers(team.ID)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}

	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}
}
