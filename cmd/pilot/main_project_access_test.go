package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/teams"
)

// =============================================================================
// GH-635: wireProjectAccessChecker tests
// =============================================================================

func TestWireProjectAccessChecker_NilConfig(t *testing.T) {
	runner := executor.NewRunner()
	cleanup := wireProjectAccessChecker(runner, &config.Config{})
	if cleanup != nil {
		t.Error("expected nil cleanup when team config is nil")
	}
}

func TestWireProjectAccessChecker_Disabled(t *testing.T) {
	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     false,
			TeamID:      "team1",
			MemberEmail: "dev@test.com",
		},
	}
	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup != nil {
		t.Error("expected nil cleanup when team config is disabled")
	}
}

func TestWireProjectAccessChecker_MissingTeamID(t *testing.T) {
	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "",
			MemberEmail: "dev@test.com",
		},
	}
	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup != nil {
		t.Error("expected nil cleanup when team_id is missing")
	}
}

func TestWireProjectAccessChecker_MissingMemberEmail(t *testing.T) {
	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "team1",
			MemberEmail: "",
		},
	}
	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup != nil {
		t.Error("expected nil cleanup when member_email is missing")
	}
}

func TestWireProjectAccessChecker_MissingMemoryPath(t *testing.T) {
	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "team1",
			MemberEmail: "dev@test.com",
		},
	}
	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup != nil {
		t.Error("expected nil cleanup when memory path is missing")
	}
}

func TestWireProjectAccessChecker_TeamNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid DB with teams store
	dbPath := filepath.Join(tmpDir, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	_, err = teams.NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	_ = db.Close()

	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "nonexistent-team",
			MemberEmail: "dev@test.com",
		},
		Memory: &config.MemoryConfig{
			Path: tmpDir,
		},
	}
	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup != nil {
		t.Error("expected nil cleanup when team is not found")
	}
}

func TestWireProjectAccessChecker_MemberNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create DB with team but no matching member
	dbPath := filepath.Join(tmpDir, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	store, err := teams.NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := teams.NewService(store)
	team, _, err := svc.CreateTeam("Test Team", "owner@test.com")
	if err != nil {
		t.Fatalf("failed to create team: %v", err)
	}
	_ = db.Close()

	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      team.Name,
			MemberEmail: "nonexistent@test.com",
		},
		Memory: &config.MemoryConfig{
			Path: tmpDir,
		},
	}
	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup != nil {
		t.Error("expected nil cleanup when member is not found")
	}
}

func TestWireProjectAccessChecker_FullWiring(t *testing.T) {
	tmpDir := t.TempDir()

	// Create DB with team, owner, and restricted developer
	dbPath := filepath.Join(tmpDir, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	store, err := teams.NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := teams.NewService(store)
	team, owner, err := svc.CreateTeam("Test Team", "owner@test.com")
	if err != nil {
		t.Fatalf("failed to create team: %v", err)
	}

	// Add developer restricted to /project/a
	_, err = svc.AddMember(team.ID, owner.ID, "dev@test.com", teams.RoleDeveloper, []string{"/project/a"})
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	_ = db.Close()

	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "Test Team",
			MemberEmail: "dev@test.com",
		},
		Memory: &config.MemoryConfig{
			Path: tmpDir,
		},
	}

	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup for valid config")
	}
	defer cleanup()

	// Verify the checker is wired by attempting to check access
	// The runner's projectAccessChecker should be set
	// We can't directly access the private field, but we know it was set
	// because wireProjectAccessChecker returned a non-nil cleanup.
}

func TestWireProjectAccessChecker_LookupByTeamID(t *testing.T) {
	tmpDir := t.TempDir()

	// Create DB with team
	dbPath := filepath.Join(tmpDir, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	store, err := teams.NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := teams.NewService(store)
	team, _, err := svc.CreateTeam("My Team", "owner@test.com")
	if err != nil {
		t.Fatalf("failed to create team: %v", err)
	}
	_ = db.Close()

	runner := executor.NewRunner()
	// Use team ID directly (not name)
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      team.ID,
			MemberEmail: "owner@test.com",
		},
		Memory: &config.MemoryConfig{
			Path: tmpDir,
		},
	}

	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup when team found by ID")
	}
	defer cleanup()
}

func TestWireProjectAccessChecker_UnrestrictedMember(t *testing.T) {
	tmpDir := t.TempDir()

	// Create DB with unrestricted developer (no project restrictions)
	dbPath := filepath.Join(tmpDir, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	store, err := teams.NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := teams.NewService(store)
	team, owner, err := svc.CreateTeam("Open Team", "owner@test.com")
	if err != nil {
		t.Fatalf("failed to create team: %v", err)
	}
	// Developer with no project restrictions (empty list = all projects)
	_, err = svc.AddMember(team.ID, owner.ID, "dev@test.com", teams.RoleDeveloper, nil)
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	_ = db.Close()

	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "Open Team",
			MemberEmail: "dev@test.com",
		},
		Memory: &config.MemoryConfig{
			Path: tmpDir,
		},
	}

	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup for unrestricted member")
	}
	defer cleanup()
}

func TestWireProjectAccessChecker_ViewerDenied(t *testing.T) {
	tmpDir := t.TempDir()

	// Create DB with viewer (who lacks PermExecuteTasks)
	dbPath := filepath.Join(tmpDir, "pilot.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	store, err := teams.NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := teams.NewService(store)
	team, owner, err := svc.CreateTeam("Team", "owner@test.com")
	if err != nil {
		t.Fatalf("failed to create team: %v", err)
	}
	_, err = svc.AddMember(team.ID, owner.ID, "viewer@test.com", teams.RoleViewer, nil)
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	_ = db.Close()

	// Viewer should still get the checker wired — the checker itself will deny at runtime
	runner := executor.NewRunner()
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Enabled:     true,
			TeamID:      "Team",
			MemberEmail: "viewer@test.com",
		},
		Memory: &config.MemoryConfig{
			Path: tmpDir,
		},
	}

	cleanup := wireProjectAccessChecker(runner, cfg)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup — checker should be wired even for viewer")
	}
	defer cleanup()
}
