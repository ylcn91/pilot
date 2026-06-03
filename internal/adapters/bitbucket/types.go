package bitbucket

import "time"

// defaultBaseURL is the Bitbucket Cloud REST 2.0 API base. Server/Data Center
// support is reserved for later via the BaseURL config seam; this adapter
// targets Cloud only for now.
const defaultBaseURL = "https://api.bitbucket.org/2.0"

// Config holds Bitbucket Cloud adapter configuration
type Config struct {
	Enabled       bool           `yaml:"enabled"`
	Token         string         `yaml:"token"`          // App password or access token (Bearer/Basic auth)
	Username      string         `yaml:"username"`       // Optional: enables Basic auth (username + app password)
	Workspace     string         `yaml:"workspace"`      // Bitbucket workspace slug
	Repo          string         `yaml:"repo"`           // Repository slug
	BaseURL       string         `yaml:"base_url"`       // Default: https://api.bitbucket.org/2.0 (Server seam reserved)
	PilotLabel    string         `yaml:"pilot_label"`    // Issue label/kind that triggers Pilot
	Polling       *PollingConfig `yaml:"polling"`        // Polling settings
	WebhookSecret string         `yaml:"webhook_secret"` // HMAC-SHA256 secret for X-Hub-Signature verification
}

// PollingConfig holds Bitbucket polling settings
type PollingConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"` // Poll interval (default 30s)
	Label    string        `yaml:"label"`    // Label to watch for (default: pilot)
}

// DefaultConfig returns default Bitbucket configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:    false,
		BaseURL:    defaultBaseURL,
		PilotLabel: "pilot",
		Polling: &PollingConfig{
			Enabled:  false,
			Interval: 30 * time.Second,
			Label:    "pilot",
		},
	}
}

// ListIssuesOptions holds options for listing issues
type ListIssuesOptions struct {
	Labels    []string
	State     string // new, open, resolved, closed, etc. (Bitbucket issue states)
	Sort      string // e.g. created_on, -created_on
	UpdatedAt time.Time
}

// Issue states (Bitbucket Cloud issue tracker terminology)
const (
	StateNew      = "new"
	StateOpen     = "open"
	StateResolved = "resolved"
	StateClosed   = "closed"
)

// Label names used by Pilot
const (
	LabelInProgress = "pilot-in-progress"
	LabelDone       = "pilot-done"
	LabelFailed     = "pilot-failed"
)

// Priority mapping from Bitbucket labels
type Priority int

const (
	PriorityNone   Priority = 0
	PriorityUrgent Priority = 1
	PriorityHigh   Priority = 2
	PriorityMedium Priority = 3
	PriorityLow    Priority = 4
)

// PriorityFromLabel converts a Bitbucket label to priority
func PriorityFromLabel(label string) Priority {
	switch label {
	case "priority::urgent", "P0":
		return PriorityUrgent
	case "priority::high", "P1":
		return PriorityHigh
	case "priority::medium", "P2":
		return PriorityMedium
	case "priority::low", "P3":
		return PriorityLow
	default:
		return PriorityNone
	}
}

// PriorityName returns the human-readable priority name
func PriorityName(priority Priority) string {
	switch priority {
	case PriorityUrgent:
		return "Urgent"
	case PriorityHigh:
		return "High"
	case PriorityMedium:
		return "Medium"
	case PriorityLow:
		return "Low"
	default:
		return "No Priority"
	}
}

// Pull request states (Bitbucket Cloud uppercase terminology)
const (
	PRStateOpen       = "OPEN"
	PRStateMerged     = "MERGED"
	PRStateDeclined   = "DECLINED"
	PRStateSuperseded = "SUPERSEDED"
)

// Commit build statuses (Bitbucket Cloud commit status states)
const (
	BuildSuccessful = "SUCCESSFUL"
	BuildFailed     = "FAILED"
	BuildInProgress = "INPROGRESS"
	BuildStopped    = "STOPPED"
)

// Links holds the hyperlink section common to most Bitbucket resources
type Links struct {
	Self *Link `json:"self,omitempty"`
	HTML *Link `json:"html,omitempty"`
}

// Link is a single Bitbucket hyperlink
type Link struct {
	Href string `json:"href"`
}

// Content represents the rich-text content of an issue
type Content struct {
	Raw    string `json:"raw"`
	Markup string `json:"markup,omitempty"`
	HTML   string `json:"html,omitempty"`
}

// Account represents a Bitbucket user account
type Account struct {
	UUID        string `json:"uuid"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
	AccountID   string `json:"account_id"`
}

// Repository represents a Bitbucket repository
type Repository struct {
	UUID       string  `json:"uuid"`
	Name       string  `json:"name"`
	FullName   string  `json:"full_name"` // workspace/repo
	Links      *Links  `json:"links,omitempty"`
	MainBranch *Branch `json:"mainbranch,omitempty"`
}

// Branch represents a repository branch reference
type Branch struct {
	Name string `json:"name"`
}

// Issue represents a Bitbucket Cloud issue
type Issue struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Content   *Content  `json:"content,omitempty"`
	State     string    `json:"state"`    // new, open, resolved, closed, ...
	Priority  string    `json:"priority"` // trivial, minor, major, critical, blocker
	Kind      string    `json:"kind"`     // bug, enhancement, proposal, task
	Links     *Links    `json:"links,omitempty"`
	Reporter  *Account  `json:"reporter,omitempty"`
	Assignee  *Account  `json:"assignee,omitempty"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
	// Labels is synthesized from kind/priority/component since the Cloud issue
	// tracker has no free-form label collection; populated by the converter.
	Labels []string `json:"-"`
}

// IssueList is the paginated envelope returned by list endpoints
type IssueList struct {
	Values  []*Issue `json:"values"`
	Page    int      `json:"page"`
	Size    int      `json:"size"`
	PageLen int      `json:"pagelen"`
	Next    string   `json:"next,omitempty"`
}

// Comment represents an issue or PR comment
type Comment struct {
	ID      int      `json:"id"`
	Content *Content `json:"content,omitempty"`
}

// PullRequest represents a Bitbucket Cloud pull request
type PullRequest struct {
	ID                int         `json:"id"`
	Title             string      `json:"title"`
	Description       string      `json:"description,omitempty"`
	State             string      `json:"state"` // OPEN, MERGED, DECLINED, SUPERSEDED
	Source            *PREndpoint `json:"source,omitempty"`
	Destination       *PREndpoint `json:"destination,omitempty"`
	Links             *Links      `json:"links,omitempty"`
	Author            *Account    `json:"author,omitempty"`
	CreatedOn         time.Time   `json:"created_on"`
	UpdatedOn         time.Time   `json:"updated_on"`
	CloseSourceBranch bool        `json:"close_source_branch,omitempty"`
	MergeCommit       *PRCommit   `json:"merge_commit,omitempty"`
}

// PREndpoint describes the source/destination side of a pull request
type PREndpoint struct {
	Branch     *Branch     `json:"branch,omitempty"`
	Commit     *PRCommit   `json:"commit,omitempty"`
	Repository *Repository `json:"repository,omitempty"`
}

// PRCommit references a commit hash
type PRCommit struct {
	Hash string `json:"hash"`
}

// PullRequestInput is used for creating pull requests
type PullRequestInput struct {
	Title             string      `json:"title"`
	Description       string      `json:"description,omitempty"`
	Source            *PREndpoint `json:"source"`
	Destination       *PREndpoint `json:"destination"`
	CloseSourceBranch bool        `json:"close_source_branch,omitempty"`
}

// PullRequestList is the paginated envelope for PR list endpoints
type PullRequestList struct {
	Values  []*PullRequest `json:"values"`
	Page    int            `json:"page"`
	Size    int            `json:"size"`
	PageLen int            `json:"pagelen"`
	Next    string         `json:"next,omitempty"`
}

// CommitStatus represents a build status attached to a commit
type CommitStatus struct {
	UUID  string `json:"uuid"`
	Key   string `json:"key"`
	State string `json:"state"` // SUCCESSFUL, FAILED, INPROGRESS, STOPPED
	Name  string `json:"name"`
	URL   string `json:"url"`
}

// CommitStatusList is the paginated envelope for commit status endpoints
type CommitStatusList struct {
	Values  []*CommitStatus `json:"values"`
	Page    int             `json:"page"`
	Size    int             `json:"size"`
	PageLen int             `json:"pagelen"`
	Next    string          `json:"next,omitempty"`
}

// Webhook event keys (Bitbucket Cloud X-Event-Key header values)
const (
	WebhookEventIssueCreated = "issue:created"
	WebhookEventIssueUpdated = "issue:updated"
	WebhookEventPRCreated    = "pullrequest:created"
	WebhookEventPRUpdated    = "pullrequest:updated"
	WebhookEventPRMerged     = "pullrequest:fulfilled"
	WebhookEventPRDeclined   = "pullrequest:rejected"
)

// WebhookPayload is the envelope Bitbucket Cloud posts for webhook events
type WebhookPayload struct {
	Repository  *Repository  `json:"repository,omitempty"`
	Actor       *Account     `json:"actor,omitempty"`
	Issue       *Issue       `json:"issue,omitempty"`
	PullRequest *PullRequest `json:"pullrequest,omitempty"`
}
