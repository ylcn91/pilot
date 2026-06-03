package teams

import "testing"

// GH-783: ResolveSlackIdentity must fall back to the slack_user_id mapping when
// email resolution fails to find a member. This proves the fallback resolves a
// member whose email does not match the lookup but whose slack_user_id does.
func TestService_ResolveSlackIdentity_SlackUserIDFallback(t *testing.T) {
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
	dev.SlackUserID = "U12345678"
	if err := store.UpdateMember(dev); err != nil {
		t.Fatalf("UpdateMember failed: %v", err)
	}

	// Email does not match any member, but slack_user_id does: must fall back.
	got, err := svc.ResolveSlackIdentity("U12345678", "no-such@example.com")
	if err != nil {
		t.Fatalf("ResolveSlackIdentity failed: %v", err)
	}
	if got != dev.ID {
		t.Errorf("got memberID %q, want %q (slackUserID fallback)", got, dev.ID)
	}
}
