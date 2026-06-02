package teams

import (
	"fmt"
)

// Service provides team management operations with permission checking
type Service struct {
	store *Store
}

// NewService creates a new team service
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Error types
var (
	ErrTeamNotFound      = fmt.Errorf("team not found")
	ErrMemberNotFound    = fmt.Errorf("member not found")
	ErrPermissionDenied  = fmt.Errorf("permission denied")
	ErrInvalidRole       = fmt.Errorf("invalid role")
	ErrCannotRemoveOwner = fmt.Errorf("cannot remove team owner")
	ErrLastOwner         = fmt.Errorf("cannot remove last owner")
	ErrAlreadyMember     = fmt.Errorf("already a member")
	ErrSelfRoleChange    = fmt.Errorf("cannot change own role")
)
