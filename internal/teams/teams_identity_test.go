package teams

import "testing"

func TestNewTeam(t *testing.T) {
	team, owner := NewTeam("Test Team", "owner@test.com")

	if team.Name != "Test Team" {
		t.Errorf("got name %q, want %q", team.Name, "Test Team")
	}
	if team.ID == "" {
		t.Error("team ID should not be empty")
	}
	if team.Settings.MaxConcurrentTasks != 2 {
		t.Errorf("default MaxConcurrentTasks should be 2, got %d", team.Settings.MaxConcurrentTasks)
	}
	if team.Settings.DefaultBranch != "main" {
		t.Errorf("default DefaultBranch should be 'main', got %q", team.Settings.DefaultBranch)
	}

	if owner.Role != RoleOwner {
		t.Errorf("owner role should be 'owner', got %s", owner.Role)
	}
	if owner.Email != "owner@test.com" {
		t.Errorf("owner email should be 'owner@test.com', got %q", owner.Email)
	}
	if owner.TeamID != team.ID {
		t.Error("owner should belong to the team")
	}
}

func TestNewMember(t *testing.T) {
	member := NewMember("team-123", "dev@test.com", RoleDeveloper, "inviter-id")

	if member.TeamID != "team-123" {
		t.Errorf("got team ID %q, want %q", member.TeamID, "team-123")
	}
	if member.Email != "dev@test.com" {
		t.Errorf("got email %q, want %q", member.Email, "dev@test.com")
	}
	if member.Role != RoleDeveloper {
		t.Errorf("got role %s, want developer", member.Role)
	}
	if member.InvitedBy != "inviter-id" {
		t.Errorf("got invited by %q, want %q", member.InvitedBy, "inviter-id")
	}
	if member.ID == "" {
		t.Error("member ID should not be empty")
	}
}

// =============================================================================
// Edge Cases and Error Scenarios
// =============================================================================

func TestService_ResolveGitHubIdentity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := NewService(store)

	team, owner, err := svc.CreateTeam("Test Team", "owner@example.com")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Add a member with GitHub username
	dev, err := svc.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	dev.GitHubUser = "octocat"
	if err := store.UpdateMember(dev); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	tests := []struct {
		name    string
		ghUser  string
		email   string
		wantID  string
		wantErr bool
	}{
		{
			name:   "resolve by GitHub username",
			ghUser: "octocat",
			email:  "",
			wantID: dev.ID,
		},
		{
			name:   "resolve by email fallback",
			ghUser: "unknown-user",
			email:  "dev@example.com",
			wantID: dev.ID,
		},
		{
			name:   "GitHub username takes priority over email",
			ghUser: "octocat",
			email:  "wrong@example.com",
			wantID: dev.ID,
		},
		{
			name:   "no match returns empty",
			ghUser: "unknown-user",
			email:  "unknown@example.com",
			wantID: "",
		},
		{
			name:   "empty inputs return empty",
			ghUser: "",
			email:  "",
			wantID: "",
		},
		{
			name:   "resolve owner by email",
			ghUser: "",
			email:  "owner@example.com",
			wantID: owner.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ResolveGitHubIdentity(tt.ghUser, tt.email)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.wantID {
				t.Errorf("got memberID %q, want %q", got, tt.wantID)
			}
		})
	}
}

// GH-634: Tests for Telegram user ID identity mapping

func TestService_ResolveTelegramIdentity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := NewService(store)

	team, owner, err := svc.CreateTeam("Test Team", "owner@example.com")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Add a member with Telegram ID
	dev, err := svc.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	dev.TelegramID = 123456789
	if err := store.UpdateMember(dev); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	tests := []struct {
		name       string
		telegramID int64
		email      string
		wantID     string
		wantErr    bool
	}{
		{
			name:       "resolve by Telegram ID",
			telegramID: 123456789,
			email:      "",
			wantID:     dev.ID,
		},
		{
			name:       "resolve by email fallback",
			telegramID: 0,
			email:      "dev@example.com",
			wantID:     dev.ID,
		},
		{
			name:       "Telegram ID takes priority over email",
			telegramID: 123456789,
			email:      "wrong@example.com",
			wantID:     dev.ID,
		},
		{
			name:       "no match returns empty",
			telegramID: 999,
			email:      "unknown@example.com",
			wantID:     "",
		},
		{
			name:       "zero ID empty email returns empty",
			telegramID: 0,
			email:      "",
			wantID:     "",
		},
		{
			name:       "resolve owner by email",
			telegramID: 0,
			email:      "owner@example.com",
			wantID:     owner.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ResolveTelegramIdentity(tt.telegramID, tt.email)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.wantID {
				t.Errorf("got memberID %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestService_ResolveSlackIdentity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := NewService(store)

	team, owner, err := svc.CreateTeam("Test Team", "owner@example.com")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Add a member with a Slack user ID
	dev, err := svc.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	dev.SlackUserID = "U12345678"
	if err := store.UpdateMember(dev); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	tests := []struct {
		name        string
		slackUserID string
		email       string
		wantID      string
		wantErr     bool
	}{
		{
			name:        "resolve by email",
			slackUserID: "",
			email:       "dev@example.com",
			wantID:      dev.ID,
		},
		{
			name:        "email takes priority over slackUserID",
			slackUserID: "U12345678",
			email:       "dev@example.com",
			wantID:      dev.ID,
		},
		{
			name:        "resolve by slackUserID fallback",
			slackUserID: "U12345678",
			email:       "",
			wantID:      dev.ID,
		},
		{
			name:        "no match returns empty",
			slackUserID: "",
			email:       "unknown@example.com",
			wantID:      "",
		},
		{
			name:        "empty inputs returns empty",
			slackUserID: "",
			email:       "",
			wantID:      "",
		},
		{
			name:        "resolve owner by email",
			slackUserID: "",
			email:       "owner@example.com",
			wantID:      owner.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ResolveSlackIdentity(tt.slackUserID, tt.email)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.wantID {
				t.Errorf("got memberID %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestService_GetMemberByGitHubUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	svc := NewService(store)

	team, owner, err := svc.CreateTeam("Test Team", "owner@example.com")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	dev, err := svc.AddMember(team.ID, owner.ID, "dev@example.com", RoleDeveloper, nil)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	dev.GitHubUser = "octocat"
	_ = store.UpdateMember(dev)

	got, err := svc.GetMemberByGitHubUser(team.ID, "octocat")
	if err != nil {
		t.Fatalf("GetMemberByGitHubUser failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected member, got nil")
	}
	if got.ID != dev.ID {
		t.Errorf("got member %q, want %q", got.ID, dev.ID)
	}

	// Not found
	got, err = svc.GetMemberByGitHubUser(team.ID, "nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown user, got %q", got.ID)
	}
}
