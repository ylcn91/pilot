package teams

import "testing"

func TestService_GetAuditLog(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	viewer, _ := service.AddMember(team.ID, owner.ID, "viewer@example.com", RoleViewer, nil)

	tests := []struct {
		name    string
		actorID string
		limit   int
		wantErr error
	}{
		{"owner can view audit log", owner.ID, 10, nil},
		{"viewer cannot view audit log", viewer.ID, 10, ErrPermissionDenied},
		{"nonexistent actor", "nonexistent", 10, ErrMemberNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := service.GetAuditLog(team.ID, tt.actorID, tt.limit)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("GetAuditLog() error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("GetAuditLog() unexpected error: %v", err)
				}
				if entries == nil {
					t.Error("expected entries, got nil")
				}
			}
		})
	}
}

func TestService_LogTaskEvent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")

	// Log a task event
	err := service.LogTaskEvent(team.ID, owner.ID, owner.Email, "task-123", AuditTaskCreated, map[string]interface{}{
		"title": "Test Task",
	})
	if err != nil {
		t.Fatalf("LogTaskEvent failed: %v", err)
	}

	// Verify audit log
	entries, _ := service.GetAuditLog(team.ID, owner.ID, 10)
	found := false
	for _, e := range entries {
		if e.Action == AuditTaskCreated && e.ResourceID == "task-123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("task event not found in audit log")
	}
}

// =============================================================================
// Factory Function Tests
// =============================================================================

func TestService_GetTeamsForUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	// Create two teams
	team1, owner1, _ := service.CreateTeam("Team 1", "owner@example.com")
	team2, owner2, _ := service.CreateTeam("Team 2", "other@example.com")

	// Add user to both teams
	_, _ = service.AddMember(team1.ID, owner1.ID, "user@example.com", RoleDeveloper, nil)
	_, _ = service.AddMember(team2.ID, owner2.ID, "user@example.com", RoleViewer, nil)

	// Get teams for user
	memberships, err := service.GetTeamsForUser("user@example.com")
	if err != nil {
		t.Fatalf("GetTeamsForUser failed: %v", err)
	}

	if len(memberships) != 2 {
		t.Errorf("got %d memberships, want 2", len(memberships))
	}
}

// =============================================================================
// Role Tests - Extended Coverage
// =============================================================================

func TestService_GetTeamsForUser_NoMemberships(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	// User with no memberships
	memberships, err := service.GetTeamsForUser("nobody@example.com")
	if err != nil {
		t.Fatalf("GetTeamsForUser failed: %v", err)
	}
	if len(memberships) != 0 {
		t.Errorf("expected 0 memberships, got %d", len(memberships))
	}
}

func TestService_GetTeamsForUser_TeamDeleted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, _ := NewStore(db)
	service := NewService(store)

	team, owner, _ := service.CreateTeam("Test Team", "owner@example.com")
	_, _ = service.AddMember(team.ID, owner.ID, "user@example.com", RoleDeveloper, nil)

	// Verify membership exists before deletion
	memberships, err := service.GetTeamsForUser("user@example.com")
	if err != nil {
		t.Fatalf("GetTeamsForUser failed: %v", err)
	}
	if len(memberships) != 1 {
		t.Errorf("before deletion: expected 1 membership, got %d", len(memberships))
	}

	// Delete team directly via store (cascade will remove members)
	_ = store.DeleteTeam(team.ID)

	// After deletion, user should have no memberships (cascade delete)
	memberships, err = service.GetTeamsForUser("user@example.com")
	if err != nil {
		t.Fatalf("GetTeamsForUser failed: %v", err)
	}
	if len(memberships) != 0 {
		t.Errorf("after deletion: expected 0 memberships, got %d", len(memberships))
	}
}
