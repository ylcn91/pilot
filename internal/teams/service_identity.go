package teams

import (
	"fmt"
)

// ResolveGitHubIdentity resolves a GitHub username (and optional email) to a member ID
// across all teams. It tries GitHub username first, then falls back to email (GH-634).
// Returns ("", nil) when no matching member is found — callers should treat this as
// "no RBAC enforcement" rather than an error.
func (s *Service) ResolveGitHubIdentity(ghUser, email string) (string, error) {
	// Try GitHub username first (most reliable mapping)
	if ghUser != "" {
		members, err := s.store.GetMembersByGitHubUser(ghUser)
		if err != nil {
			return "", fmt.Errorf("lookup by github user %q: %w", ghUser, err)
		}
		if len(members) > 0 {
			return members[0].ID, nil
		}
	}

	// Fall back to email
	if email != "" {
		members, err := s.store.GetMembersByEmail(email)
		if err != nil {
			return "", fmt.Errorf("lookup by email %q: %w", email, err)
		}
		if len(members) > 0 {
			return members[0].ID, nil
		}
	}

	return "", nil
}

// ResolveTelegramIdentity resolves a Telegram user ID (and optional email) to a member ID
// across all teams. It tries Telegram ID first, then falls back to email (GH-634).
// Returns ("", nil) when no matching member is found — callers should treat this as
// "no RBAC enforcement" rather than an error.
func (s *Service) ResolveTelegramIdentity(telegramID int64, email string) (string, error) {
	// Try Telegram user ID first (most reliable mapping)
	if telegramID != 0 {
		members, err := s.store.GetMembersByTelegramID(telegramID)
		if err != nil {
			return "", fmt.Errorf("lookup by telegram_id %d: %w", telegramID, err)
		}
		if len(members) > 0 {
			return members[0].ID, nil
		}
	}

	// Fall back to email (from config, not from Telegram API which doesn't expose it)
	if email != "" {
		members, err := s.store.GetMembersByEmail(email)
		if err != nil {
			return "", fmt.Errorf("lookup by email %q: %w", email, err)
		}
		if len(members) > 0 {
			return members[0].ID, nil
		}
	}

	return "", nil
}

// ResolveSlackIdentity resolves a Slack user ID (and optional email) to a member ID
// across all teams. Currently uses email-only resolution; slackUserID is reserved for
// future store-level mapping when slack_user_id column is added (GH-784).
// Returns ("", nil) when no matching member is found — callers should treat this as
// "no RBAC enforcement" rather than an error.
func (s *Service) ResolveSlackIdentity(slackUserID, email string) (string, error) {
	// TODO(GH-784): When slack_user_id column is added, lookup by slackUserID first
	// For now, slackUserID is accepted but not used for store lookup
	_ = slackUserID

	// Resolve by email (Slack provides email via users.info API)
	if email != "" {
		members, err := s.store.GetMembersByEmail(email)
		if err != nil {
			return "", fmt.Errorf("lookup by email %q: %w", email, err)
		}
		if len(members) > 0 {
			return members[0].ID, nil
		}
	}

	return "", nil
}
