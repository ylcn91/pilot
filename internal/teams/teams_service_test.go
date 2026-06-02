package teams

import "testing"

func TestService_CreateTeam(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, err := service.CreateTeam("Test Team", "owner@example.com")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	if team.Name != "Test Team" {
		t.Errorf("got name %q, want %q", team.Name, "Test Team")
	}

	if owner.Role != RoleOwner {
		t.Errorf("got role %s, want owner", owner.Role)
	}
}

func TestService_GetTeam(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, _, _ := service.CreateTeam("Test Team", "owner@example.com")

	tests := []struct {
		name      string
		teamID    string
		wantErr   error
		wantFound bool
	}{
		{"existing team", team.ID, nil, true},
		{"nonexistent team", "nonexistent-id", ErrTeamNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.GetTeam(tt.teamID)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("GetTeam() error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("GetTeam() unexpected error: %v", err)
				}
				if tt.wantFound && got == nil {
					t.Error("expected team, got nil")
				}
			}
		})
	}
}

func TestService_GetTeamByName(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	_, _, _ = service.CreateTeam("Unique Name", "owner@example.com")

	got, err := service.GetTeamByName("Unique Name")
	if err != nil {
		t.Fatalf("GetTeamByName failed: %v", err)
	}
	if got == nil {
		t.Error("expected team, got nil")
	}

	notFound, _ := service.GetTeamByName("Does Not Exist")
	if notFound != nil {
		t.Error("expected nil for nonexistent name")
	}
}

func TestService_ListTeams(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	_, _, _ = service.CreateTeam("Team A", "owner@example.com")
	_, _, _ = service.CreateTeam("Team B", "owner@example.com")

	teams, err := service.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}

	if len(teams) != 2 {
		t.Errorf("got %d teams, want 2", len(teams))
	}
}

func TestService_UpdateTeamSettings(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	dev, _ := service.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)

	tests := []struct {
		name     string
		actorID  string
		settings Settings
		wantErr  error
	}{
		{
			name:    "owner can update",
			actorID: owner.ID,
			settings: Settings{
				MaxConcurrentTasks: 10,
				DefaultBranch:      "develop",
			},
			wantErr: nil,
		},
		{
			name:     "developer cannot update",
			actorID:  dev.ID,
			settings: Settings{MaxConcurrentTasks: 5},
			wantErr:  ErrPermissionDenied,
		},
		{
			name:     "nonexistent actor",
			actorID:  "nonexistent",
			settings: Settings{},
			wantErr:  ErrMemberNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.UpdateTeamSettings(team.ID, tt.actorID, tt.settings)
			if err != tt.wantErr {
				t.Errorf("UpdateTeamSettings() error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	// Verify settings were updated
	got, _ := service.GetTeam(team.ID)
	if got.Settings.MaxConcurrentTasks != 10 {
		t.Errorf("settings not updated: got %d, want 10", got.Settings.MaxConcurrentTasks)
	}
}

func TestService_UpdateTeamSettings_TeamNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Delete the team, then try to update settings
	_ = store.DeleteTeam(team.ID)

	err := service.UpdateTeamSettings(team.ID, owner.ID, Settings{})
	if err != ErrTeamNotFound {
		t.Errorf("expected ErrTeamNotFound, got %v", err)
	}
}

func TestService_DeleteTeam(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	admin, _ := service.AddMember(team.ID, owner.ID, "admin@example.com", RoleAdmin, nil)

	tests := []struct {
		name    string
		actorID string
		wantErr error
	}{
		{"admin cannot delete", admin.ID, ErrPermissionDenied},
		{"nonexistent actor", "nonexistent", ErrMemberNotFound},
		{"owner can delete", owner.ID, nil},
	}

	for _, tt := range tests {
		if tt.wantErr == nil {
			// Skip success case until last
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			err := service.DeleteTeam(team.ID, tt.actorID)
			if err != tt.wantErr {
				t.Errorf("DeleteTeam() error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	// Test successful deletion
	t.Run("owner can delete", func(t *testing.T) {
		err := service.DeleteTeam(team.ID, owner.ID)
		if err != nil {
			t.Errorf("DeleteTeam() unexpected error: %v", err)
		}
		// Verify deletion
		_, err = service.GetTeam(team.ID)
		if err != ErrTeamNotFound {
			t.Error("team should be deleted")
		}
	})
}

func TestService_DeleteTeam_TeamNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Delete team first via store
	_ = store.DeleteTeam(team.ID)

	// Now try via service
	err := service.DeleteTeam(team.ID, owner.ID)
	if err != ErrTeamNotFound {
		t.Errorf("expected ErrTeamNotFound, got %v", err)
	}
}
