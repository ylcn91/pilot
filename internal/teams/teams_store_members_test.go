package teams

import "testing"

func TestStore_AddAndListMembers(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	// Add another member
	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, owner.ID)
	if err := store.AddMember(member); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// List members
	members, err := store.ListMembers(team.ID)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}

	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}
}

func TestStore_GetMemberByEmail(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	// Get by email
	got, err := store.GetMemberByEmail(team.ID, "owner@example.com")
	if err != nil {
		t.Fatalf("GetMemberByEmail failed: %v", err)
	}

	if got == nil {
		t.Fatal("expected member, got nil")
	}

	if got.Role != RoleOwner {
		t.Errorf("got role %s, want owner", got.Role)
	}
}

func TestStore_UpdateMember(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, owner.ID)
	_ = store.AddMember(member)

	// Update role
	member.Role = RoleAdmin
	if err := store.UpdateMember(member); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	// Verify
	got, _ := store.GetMember(member.ID)
	if got.Role != RoleAdmin {
		t.Errorf("got role %s, want admin", got.Role)
	}
}

func TestStore_GetMember_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	got, err := store.GetMember("nonexistent-id")
	if err != nil {
		t.Fatalf("GetMember should not error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent member")
	}
}

func TestStore_GetMemberByEmail_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	got, err := store.GetMemberByEmail(team.ID, "nonexistent@example.com")
	if err != nil {
		t.Fatalf("GetMemberByEmail should not error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent email")
	}
}

func TestStore_GetMembersByEmail(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create two teams
	team1, _ := NewTeam("Team 1", "owner1@example.com")
	team2, _ := NewTeam("Team 2", "owner2@example.com")
	_ = store.CreateTeam(team1)
	_ = store.CreateTeam(team2)

	// Add same user to both teams
	member1 := NewMember(team1.ID, "shared@example.com", RoleDeveloper, "")
	member2 := NewMember(team2.ID, "shared@example.com", RoleViewer, "")
	_ = store.AddMember(member1)
	_ = store.AddMember(member2)

	// Get all memberships
	members, err := store.GetMembersByEmail("shared@example.com")
	if err != nil {
		t.Fatalf("GetMembersByEmail failed: %v", err)
	}

	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}
}

func TestStore_RemoveMember(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, owner.ID)
	_ = store.AddMember(member)

	// Remove member
	if err := store.RemoveMember(member.ID); err != nil {
		t.Fatalf("RemoveMember failed: %v", err)
	}

	// Verify member is gone
	got, err := store.GetMember(member.ID)
	if err != nil {
		t.Fatalf("GetMember should not error: %v", err)
	}
	if got != nil {
		t.Error("expected nil after removal")
	}
}

func TestStore_CountMembersByRole(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	// Add members with various roles
	dev1 := NewMember(team.ID, "dev1@example.com", RoleDeveloper, owner.ID)
	dev2 := NewMember(team.ID, "dev2@example.com", RoleDeveloper, owner.ID)
	viewer := NewMember(team.ID, "viewer@example.com", RoleViewer, owner.ID)
	_ = store.AddMember(dev1)
	_ = store.AddMember(dev2)
	_ = store.AddMember(viewer)

	tests := []struct {
		role     Role
		expected int
	}{
		{RoleOwner, 1},
		{RoleDeveloper, 2},
		{RoleViewer, 1},
		{RoleAdmin, 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			count, err := store.CountMembersByRole(team.ID, tt.role)
			if err != nil {
				t.Fatalf("CountMembersByRole failed: %v", err)
			}
			if count != tt.expected {
				t.Errorf("got count %d, want %d", count, tt.expected)
			}
		})
	}
}

func TestStore_MemberWithProjects(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	// Add member with project restrictions
	member := NewMember(team.ID, "restricted@example.com", RoleDeveloper, owner.ID)
	member.Projects = []string{"/project/a", "/project/b", "/project/c"}

	if err := store.AddMember(member); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Retrieve and verify
	got, err := store.GetMember(member.ID)
	if err != nil {
		t.Fatalf("GetMember failed: %v", err)
	}

	if len(got.Projects) != 3 {
		t.Errorf("got %d projects, want 3", len(got.Projects))
	}

	// Update projects
	got.Projects = []string{"/project/x"}
	if err := store.UpdateMember(got); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	got, _ = store.GetMember(member.ID)
	if len(got.Projects) != 1 || got.Projects[0] != "/project/x" {
		t.Errorf("after update: got projects %v, want [/project/x]", got.Projects)
	}
}

// =============================================================================
// Service Tests - Extended Coverage
// =============================================================================

func TestStore_ListMembers_Empty(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Empty Team", "owner@example.com")
	_ = store.CreateTeam(team)
	// Don't add any members

	members, err := store.ListMembers(team.ID)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected 0 members, got %d", len(members))
	}
}

func TestStore_MemberWithName(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	member := NewMember(team.ID, "named@example.com", RoleDeveloper, "")
	member.Name = "John Doe"

	if err := store.AddMember(member); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	got, err := store.GetMember(member.ID)
	if err != nil {
		t.Fatalf("GetMember failed: %v", err)
	}
	if got.Name != "John Doe" {
		t.Errorf("got name %q, want %q", got.Name, "John Doe")
	}

	// Update name
	got.Name = "Jane Doe"
	if err := store.UpdateMember(got); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	got, _ = store.GetMember(member.ID)
	if got.Name != "Jane Doe" {
		t.Errorf("after update: got name %q, want %q", got.Name, "Jane Doe")
	}
}
