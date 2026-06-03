package teams

import "testing"

// latestSchemaVersion is the highest version in schemaMigrations; migrate must
// advance PRAGMA user_version to exactly this after a fresh setup.
func latestSchemaVersion() int {
	v := 0
	for _, m := range schemaMigrations {
		if m.version > v {
			v = m.version
		}
	}
	return v
}

// readUserVersion reads PRAGMA user_version from the store's database.
func readUserVersion(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

// TestStore_Migrate_AdvancesUserVersion verifies that NewStore applies all
// migrations and records the latest version in PRAGMA user_version.
func TestStore_Migrate_AdvancesUserVersion(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	want := latestSchemaVersion()
	if got := readUserVersion(t, store); got != want {
		t.Errorf("user_version = %d after migrate, want %d", got, want)
	}
}

// TestStore_Migrate_Idempotent verifies that constructing a second Store on the
// same database is a no-op: the version table already covers every migration, so
// no ALTER TABLE re-runs and no "duplicate column" error escapes. This is the
// exact property the version-numbered migration table was introduced to
// guarantee (replacing the string-match-on-error idempotency hack).
func TestStore_Migrate_Idempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	first, err := NewStore(db)
	if err != nil {
		t.Fatalf("first NewStore failed: %v", err)
	}
	v1 := readUserVersion(t, first)

	// Re-running migrate on the same DB must not error and must not change
	// the recorded version.
	second, err := NewStore(db)
	if err != nil {
		t.Fatalf("second NewStore failed (migration not idempotent): %v", err)
	}
	v2 := readUserVersion(t, second)

	if v1 != v2 {
		t.Errorf("user_version changed on re-migrate: first=%d, second=%d", v1, v2)
	}
	if v2 != latestSchemaVersion() {
		t.Errorf("user_version = %d after re-migrate, want %d", v2, latestSchemaVersion())
	}

	// Data written through the first store must survive the second migrate run.
	team, _ := NewTeam("Idempotent Team", "owner@example.com")
	if err := first.CreateTeam(team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}
	got, err := second.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("GetTeam after re-migrate failed: %v", err)
	}
	if got.ID != team.ID {
		t.Errorf("team lost across re-migrate: got %q, want %q", got.ID, team.ID)
	}
}

// TestStore_Migrate_LegacyDuplicateColumnTolerated simulates a pre-version-table
// database (user_version 0) that already carries an identity column added by a
// legacy flat-list deployment. migrate must tolerate the resulting "duplicate
// column" on the ALTER TABLE and still advance the version rather than aborting.
func TestStore_Migrate_LegacyDuplicateColumnTolerated(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Apply only version 1 (base tables) to mimic an old schema, then manually
	// add a column that a later migration also adds, leaving user_version at 0.
	base := &Store{db: db}
	if err := base.applyMigration(schemaMigrations[0]); err != nil {
		t.Fatalf("apply base migration: %v", err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("reset user_version: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE team_members ADD COLUMN github_user TEXT`); err != nil {
		t.Fatalf("seed legacy column: %v", err)
	}

	// Full migrate must not abort on the duplicate github_user column.
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore over legacy schema failed: %v", err)
	}
	if got := readUserVersion(t, store); got != latestSchemaVersion() {
		t.Errorf("user_version = %d after legacy migrate, want %d", got, latestSchemaVersion())
	}

	// The Slack column from a later migration must exist (proving migrate ran
	// past the duplicate-column step instead of stopping).
	team, _ := NewTeam("Legacy Team", "owner@example.com")
	if err := store.CreateTeam(team); err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}
	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, "")
	member.SlackUserID = "U99999999"
	if err := store.AddMember(member); err != nil {
		t.Fatalf("AddMember with SlackUserID failed (slack_user_id column missing?): %v", err)
	}
	members, err := store.GetMembersBySlackUserID("U99999999")
	if err != nil {
		t.Fatalf("GetMembersBySlackUserID failed: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member by slack_user_id, got %d", len(members))
	}
}
