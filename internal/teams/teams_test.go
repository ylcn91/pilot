package teams

import "testing"

func TestStore_CreateAndGetTeam(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")

	// Create team
	if err := store.CreateTeam(team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Get team
	got, err := store.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("GetTeam failed: %v", err)
	}

	if got.Name != team.Name {
		t.Errorf("got name %q, want %q", got.Name, team.Name)
	}
}

func TestStore_GetTeamByName(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Unique Team Name", "owner@example.com")
	if err := store.CreateTeam(team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	tests := []struct {
		name       string
		teamName   string
		wantFound  bool
		wantTeamID string
	}{
		{"existing team", "Unique Team Name", true, team.ID},
		{"non-existing team", "Does Not Exist", false, ""},
		{"empty name", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetTeamByName(tt.teamName)
			if err != nil {
				t.Fatalf("GetTeamByName failed: %v", err)
			}
			if tt.wantFound && got == nil {
				t.Error("expected team to be found, got nil")
			}
			if !tt.wantFound && got != nil {
				t.Error("expected nil, got team")
			}
			if tt.wantFound && got != nil && got.ID != tt.wantTeamID {
				t.Errorf("got team ID %q, want %q", got.ID, tt.wantTeamID)
			}
		})
	}
}

func TestStore_GetTeamNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	got, err := store.GetTeam("nonexistent-id")
	if err != nil {
		t.Fatalf("GetTeam should not error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent team")
	}
}

func TestStore_UpdateTeam(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Original Name", "owner@example.com")
	if err := store.CreateTeam(team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Update team
	team.Name = "Updated Name"
	team.Settings.MaxConcurrentTasks = 5
	team.Settings.DefaultBranch = "develop"
	team.Settings.AllowedProjects = []string{"/project/a"}

	if err := store.UpdateTeam(team); err != nil {
		t.Fatalf("UpdateTeam failed: %v", err)
	}

	// Verify
	got, err := store.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("GetTeam failed: %v", err)
	}

	if got.Name != "Updated Name" {
		t.Errorf("got name %q, want %q", got.Name, "Updated Name")
	}
	if got.Settings.MaxConcurrentTasks != 5 {
		t.Errorf("got MaxConcurrentTasks %d, want 5", got.Settings.MaxConcurrentTasks)
	}
	if got.Settings.DefaultBranch != "develop" {
		t.Errorf("got DefaultBranch %q, want %q", got.Settings.DefaultBranch, "develop")
	}
	if len(got.Settings.AllowedProjects) != 1 || got.Settings.AllowedProjects[0] != "/project/a" {
		t.Errorf("got AllowedProjects %v, want [/project/a]", got.Settings.AllowedProjects)
	}
}

func TestStore_DeleteTeam(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Team To Delete", "owner@example.com")
	if err := store.CreateTeam(team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}
	if err := store.AddMember(owner); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Delete team
	if err := store.DeleteTeam(team.ID); err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}

	// Verify team is gone
	got, err := store.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("GetTeam should not error: %v", err)
	}
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestStore_ListTeams(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create multiple teams
	teamNames := []string{"Alpha Team", "Beta Team", "Gamma Team"}
	for _, name := range teamNames {
		team, _ := NewTeam(name, "owner@example.com")
		if err := store.CreateTeam(team); err != nil {
			t.Fatalf("CreateTeam failed for %s: %v", name, err)
		}
	}

	// List teams
	teams, err := store.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}

	if len(teams) != 3 {
		t.Errorf("got %d teams, want 3", len(teams))
	}

	// Verify sorted by name
	if teams[0].Name != "Alpha Team" || teams[1].Name != "Beta Team" || teams[2].Name != "Gamma Team" {
		t.Error("teams not sorted by name")
	}
}

func TestStore_ListTeams_Empty(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	teams, err := store.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams failed: %v", err)
	}
	if len(teams) != 0 {
		t.Errorf("expected 0 teams, got %d", len(teams))
	}
}

func TestStore_AuditLog(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	// Add audit entry
	entry := &AuditEntry{
		ID:         "audit-1",
		TeamID:     team.ID,
		ActorID:    owner.ID,
		ActorEmail: owner.Email,
		Action:     AuditMemberAdded,
		Resource:   "member",
		ResourceID: "member-1",
		Details:    map[string]interface{}{"email": "new@example.com"},
	}
	entry.CreatedAt = owner.JoinedAt

	if err := store.AddAuditEntry(entry); err != nil {
		t.Fatalf("AddAuditEntry failed: %v", err)
	}

	// Get audit log
	entries, err := store.GetAuditLog(team.ID, 10)
	if err != nil {
		t.Fatalf("GetAuditLog failed: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}

	if entries[0].Action != AuditMemberAdded {
		t.Errorf("got action %s, want member.added", entries[0].Action)
	}
}

func TestStore_AuditLog_WithNilDetails(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, owner := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)
	_ = store.AddMember(owner)

	// Add entry with nil details
	entry := &AuditEntry{
		ID:         "audit-nil",
		TeamID:     team.ID,
		ActorID:    owner.ID,
		ActorEmail: owner.Email,
		Action:     AuditProjectRemoved,
		Resource:   "project",
		ResourceID: "",
		Details:    nil,
		CreatedAt:  owner.JoinedAt,
	}

	if err := store.AddAuditEntry(entry); err != nil {
		t.Fatalf("AddAuditEntry with nil details failed: %v", err)
	}

	entries, err := store.GetAuditLog(team.ID, 10)
	if err != nil {
		t.Fatalf("GetAuditLog failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}
