package teams

import "testing"

func TestStore_MemberWithGitHubUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, "")
	member.GitHubUser = "octocat"

	if err := store.AddMember(member); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Verify persisted via GetMember
	got, err := store.GetMember(member.ID)
	if err != nil {
		t.Fatalf("GetMember failed: %v", err)
	}
	if got.GitHubUser != "octocat" {
		t.Errorf("got GitHubUser %q, want %q", got.GitHubUser, "octocat")
	}

	// Update GitHub username
	got.GitHubUser = "octocat-v2"
	if err := store.UpdateMember(got); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	got, _ = store.GetMember(member.ID)
	if got.GitHubUser != "octocat-v2" {
		t.Errorf("after update: got GitHubUser %q, want %q", got.GitHubUser, "octocat-v2")
	}
}

func TestStore_GetMemberByGitHubUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, "")
	member.GitHubUser = "octocat"
	_ = store.AddMember(member)

	// Found
	got, err := store.GetMemberByGitHubUser(team.ID, "octocat")
	if err != nil {
		t.Fatalf("GetMemberByGitHubUser failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected member, got nil")
	}
	if got.ID != member.ID {
		t.Errorf("got member ID %q, want %q", got.ID, member.ID)
	}

	// Not found — wrong team
	got, err = store.GetMemberByGitHubUser("wrong-team", "octocat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got member %q", got.ID)
	}

	// Not found — wrong username
	got, err = store.GetMemberByGitHubUser(team.ID, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got member %q", got.ID)
	}
}

func TestStore_GetMembersByGitHubUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Two teams, same GitHub user
	team1, _ := NewTeam("Team A", "owner1@example.com")
	team2, _ := NewTeam("Team B", "owner2@example.com")
	_ = store.CreateTeam(team1)
	_ = store.CreateTeam(team2)

	m1 := NewMember(team1.ID, "dev@example.com", RoleDeveloper, "")
	m1.GitHubUser = "octocat"
	_ = store.AddMember(m1)

	m2 := NewMember(team2.ID, "dev@example.com", RoleDeveloper, "")
	m2.GitHubUser = "octocat"
	_ = store.AddMember(m2)

	members, err := store.GetMembersByGitHubUser("octocat")
	if err != nil {
		t.Fatalf("GetMembersByGitHubUser failed: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}

	// No matches
	members, err = store.GetMembersByGitHubUser("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("got %d members, want 0", len(members))
	}
}

func TestStore_MemberWithTelegramID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, "")
	member.TelegramID = 123456789

	if err := store.AddMember(member); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Verify persisted via GetMember
	got, err := store.GetMember(member.ID)
	if err != nil {
		t.Fatalf("GetMember failed: %v", err)
	}
	if got.TelegramID != 123456789 {
		t.Errorf("got TelegramID %d, want 123456789", got.TelegramID)
	}

	// Update Telegram ID
	got.TelegramID = 987654321
	if err := store.UpdateMember(got); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	got, _ = store.GetMember(member.ID)
	if got.TelegramID != 987654321 {
		t.Errorf("after update: got TelegramID %d, want 987654321", got.TelegramID)
	}
}

func TestStore_GetMemberByTelegramID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	team, _ := NewTeam("Test Team", "owner@example.com")
	_ = store.CreateTeam(team)

	member := NewMember(team.ID, "dev@example.com", RoleDeveloper, "")
	member.TelegramID = 123456789
	_ = store.AddMember(member)

	// Found
	got, err := store.GetMemberByTelegramID(team.ID, 123456789)
	if err != nil {
		t.Fatalf("GetMemberByTelegramID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected member, got nil")
	}
	if got.ID != member.ID {
		t.Errorf("got member ID %q, want %q", got.ID, member.ID)
	}

	// Not found — wrong team
	got, err = store.GetMemberByTelegramID("wrong-team", 123456789)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got member %q", got.ID)
	}

	// Not found — wrong ID
	got, err = store.GetMemberByTelegramID(team.ID, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got member %q", got.ID)
	}
}

func TestStore_GetMembersByTelegramID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Two teams, same Telegram user ID
	team1, _ := NewTeam("Team A", "owner1@example.com")
	team2, _ := NewTeam("Team B", "owner2@example.com")
	_ = store.CreateTeam(team1)
	_ = store.CreateTeam(team2)

	m1 := NewMember(team1.ID, "dev@example.com", RoleDeveloper, "")
	m1.TelegramID = 123456789
	_ = store.AddMember(m1)

	m2 := NewMember(team2.ID, "dev@example.com", RoleDeveloper, "")
	m2.TelegramID = 123456789
	_ = store.AddMember(m2)

	members, err := store.GetMembersByTelegramID(123456789)
	if err != nil {
		t.Fatalf("GetMembersByTelegramID failed: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}

	// No matches
	members, err = store.GetMembersByTelegramID(999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("got %d members, want 0", len(members))
	}

	// Zero ID returns no results (filtered by telegram_id != 0)
	members, err = store.GetMembersByTelegramID(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("got %d members for ID 0, want 0", len(members))
	}
}
