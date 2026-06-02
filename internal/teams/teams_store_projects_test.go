package teams

import "testing"

// GH-634: Tests for GitHub username identity mapping

func TestStore_ProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	// Set project access
	access := &ProjectAccess{
		TeamID:      team.ID,
		ProjectPath: "/project/alpha",
		DefaultRole: RoleDeveloper,
	}
	if err := store.SetProjectAccess(access); err != nil {
		t.Fatalf("SetProjectAccess failed: %v", err)
	}

	// Get project access
	got, err := store.GetProjectAccess(team.ID, "/project/alpha")
	if err != nil {
		t.Fatalf("GetProjectAccess failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected project access, got nil")
	}
	if got.DefaultRole != RoleDeveloper {
		t.Errorf("got role %s, want developer", got.DefaultRole)
	}

	// Update project access (upsert)
	access.DefaultRole = RoleViewer
	if err := store.SetProjectAccess(access); err != nil {
		t.Fatalf("SetProjectAccess (upsert) failed: %v", err)
	}

	got, _ = store.GetProjectAccess(team.ID, "/project/alpha")
	if got.DefaultRole != RoleViewer {
		t.Errorf("after upsert: got role %s, want viewer", got.DefaultRole)
	}
}

func TestStore_GetProjectAccess_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	got, err := store.GetProjectAccess(team.ID, "/nonexistent/path")
	if err != nil {
		t.Fatalf("GetProjectAccess should not error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent project access")
	}
}

func TestStore_ListProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	// Add multiple project accesses
	projects := []string{"/project/alpha", "/project/beta", "/project/gamma"}
	for _, path := range projects {
		access := &ProjectAccess{
			TeamID:      team.ID,
			ProjectPath: path,
			DefaultRole: RoleDeveloper,
		}
		_ = store.SetProjectAccess(access)
	}

	// List project access
	accesses, err := store.ListProjectAccess(team.ID)
	if err != nil {
		t.Fatalf("ListProjectAccess failed: %v", err)
	}

	if len(accesses) != 3 {
		t.Errorf("got %d accesses, want 3", len(accesses))
	}

	// Verify sorted by project_path
	if accesses[0].ProjectPath != "/project/alpha" {
		t.Error("project accesses not sorted by path")
	}
}

func TestStore_RemoveProjectAccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	access := &ProjectAccess{
		TeamID:      team.ID,
		ProjectPath: "/project/to-remove",
		DefaultRole: RoleDeveloper,
	}
	_ = store.SetProjectAccess(access)

	// Remove project access
	if err := store.RemoveProjectAccess(team.ID, "/project/to-remove"); err != nil {
		t.Fatalf("RemoveProjectAccess failed: %v", err)
	}

	// Verify removal
	got, _ := store.GetProjectAccess(team.ID, "/project/to-remove")
	if got != nil {
		t.Error("expected nil after removal")
	}
}
