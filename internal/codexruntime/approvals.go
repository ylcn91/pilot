package codexruntime

type ReviewDecision string
type CommandExecutionApprovalDecision string
type FileChangeApprovalDecision string
type PermissionGrantScope string

const (
	ReviewApproved           ReviewDecision = "approved"
	ReviewApprovedForSession ReviewDecision = "approved_for_session"
	ReviewDenied             ReviewDecision = "denied"
	ReviewTimedOut           ReviewDecision = "timed_out"
	ReviewAbort              ReviewDecision = "abort"

	CommandExecutionAccept           CommandExecutionApprovalDecision = "accept"
	CommandExecutionAcceptForSession CommandExecutionApprovalDecision = "acceptForSession"
	CommandExecutionDecline          CommandExecutionApprovalDecision = "decline"
	CommandExecutionCancel           CommandExecutionApprovalDecision = "cancel"

	FileChangeAccept           FileChangeApprovalDecision = "accept"
	FileChangeAcceptForSession FileChangeApprovalDecision = "acceptForSession"
	FileChangeDecline          FileChangeApprovalDecision = "decline"
	FileChangeCancel           FileChangeApprovalDecision = "cancel"

	PermissionGrantTurn    PermissionGrantScope = "turn"
	PermissionGrantSession PermissionGrantScope = "session"
)

type ReviewApprovalResponse struct {
	Decision ReviewDecision `json:"decision"`
}

type CommandExecutionApprovalResponse struct {
	Decision CommandExecutionApprovalDecision `json:"decision"`
}

type FileChangeApprovalResponse struct {
	Decision FileChangeApprovalDecision `json:"decision"`
}

type GrantedPermissionProfile struct {
	Network    any `json:"network,omitempty"`
	FileSystem any `json:"fileSystem,omitempty"`
}

type PermissionsApprovalResponse struct {
	Permissions      GrantedPermissionProfile `json:"permissions"`
	Scope            PermissionGrantScope     `json:"scope"`
	StrictAutoReview bool                     `json:"strictAutoReview,omitempty"`
}

func (c *Client) RespondReviewApproval(request Message, decision ReviewDecision) error {
	return c.Respond(request, ReviewApprovalResponse{Decision: decision})
}

func (c *Client) RespondCommandExecutionApproval(request Message, decision CommandExecutionApprovalDecision) error {
	return c.Respond(request, CommandExecutionApprovalResponse{Decision: decision})
}

func (c *Client) RespondFileChangeApproval(request Message, decision FileChangeApprovalDecision) error {
	return c.Respond(request, FileChangeApprovalResponse{Decision: decision})
}

func (c *Client) RespondPermissionsApproval(request Message, response PermissionsApprovalResponse) error {
	return c.Respond(request, response)
}
