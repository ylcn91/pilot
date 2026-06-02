package executor

import (
	"context"
	"encoding/json"
	"time"
)

// StreamEvent represents a Claude Code stream-json event
type StreamEvent struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype,omitempty"`
	Message       *AssistantMsg   `json:"message,omitempty"`
	Result        string          `json:"result,omitempty"`
	IsError       bool            `json:"is_error,omitempty"`
	DurationMS    int             `json:"duration_ms,omitempty"`
	NumTurns      int             `json:"num_turns,omitempty"`
	ToolUseResult json.RawMessage `json:"tool_use_result,omitempty"`
	// Token usage (TASK-13)
	Usage *UsageInfo `json:"usage,omitempty"`
	Model string     `json:"model,omitempty"`
	// Session ID for resume support (GH-1265)
	SessionID string `json:"session_id,omitempty"`
}

// UsageInfo represents token usage in stream events
type UsageInfo struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

// AssistantMsg represents the message field in assistant events
type AssistantMsg struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents content in assistant messages.
// Also used for tool_result blocks in user messages (Qwen Code sends these
// in message.content[] instead of Claude Code's flat tool_use_result field).
type ContentBlock struct {
	Type    string                 `json:"type"`
	Text    string                 `json:"text,omitempty"`
	Content string                 `json:"content,omitempty"` // For tool_result blocks
	Name    string                 `json:"name,omitempty"`
	Input   map[string]interface{} `json:"input,omitempty"`
	IsError bool                   `json:"is_error,omitempty"` // For tool_result blocks
}

// ToolResultContent represents tool result in user events
type ToolResultContent struct {
	ToolUseID string `json:"tool_use_id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

// progressState tracks execution phase for compact progress reporting
type progressState struct {
	phase        string   // Current phase: Exploring, Implementing, Testing, Committing
	filesRead    int      // Count of files read
	filesWrite   int      // Count of files written
	commands     int      // Count of bash commands
	hasNavigator bool     // Project has Navigator
	navPhase     string   // Navigator phase: INIT, RESEARCH, IMPL, VERIFY, COMPLETE
	navIteration int      // Navigator loop iteration
	navProgress  int      // Navigator-reported progress
	exitSignal   bool     // Navigator EXIT_SIGNAL detected
	commitSHAs   []string // Extracted commit SHAs from git output
	// Metrics tracking (TASK-13)
	tokensInput              int64  // Input tokens used
	tokensOutput             int64  // Output tokens used
	cacheCreationInputTokens int64  // Cache creation input tokens (GH-2164)
	cacheReadInputTokens     int64  // Cache read input tokens (GH-2164)
	modelName                string // Model used
	// Note: filesChanged/linesAdded/linesRemoved tracked via git diff at commit time
	// Intent judge retry tracking (GH-624)
	intentRetried bool // Set after first intent retry to prevent infinite loops
	// Budget enforcement (GH-539)
	budgetExceeded bool               // Set when per-task token/duration limit is exceeded
	budgetReason   string             // Human-readable reason for budget cancellation
	budgetCancel   context.CancelFunc // Cancel function to terminate execution on budget breach
	// Stall detection (TASK-308)
	stallDetected bool // Set by stall watchdog when no event for stall_timeout
	// Smart retry tracking (GH-920)
	smartRetryAttempt int // Current retry attempt for error-based retries
	// Session resume support (GH-1265)
	sessionID string // Claude Code session ID for resume in self-review
	// Modified files tracking (GH-1388)
	modifiedFiles []string // List of actually modified files from Write/Edit tool events
}

// Task represents a task to be executed by the Runner.
// It contains all the information needed to execute a development task
// using Claude Code, including project context, branching options, and PR creation settings.
type Task struct {
	// ID is the unique identifier for this task (e.g., "TASK-123").
	ID string
	// Title is the human-readable title of the task.
	Title string
	// Description contains the full task description and requirements.
	Description string
	// Priority indicates task priority (lower numbers = higher priority).
	Priority int
	// ProjectPath is the absolute path to the project directory.
	ProjectPath string
	// Branch is the git branch name to create for this task (optional).
	Branch string
	// Verbose enables streaming Claude Code output to console when true.
	Verbose bool
	// CreatePR enables automatic GitHub PR creation after successful execution.
	CreatePR bool
	// BaseBranch specifies the base branch for PR creation (defaults to main/master).
	BaseBranch string
	// ImagePath is the path to an image file for multimodal analysis tasks (optional).
	ImagePath string
	// DirectCommit enables pushing directly to main without branches or PRs.
	// Requires executor.direct_commit=true in config AND --direct-commit flag.
	DirectCommit bool
	// SourceRepo is the source repository in "owner/repo" format (GH-386).
	// Used for cross-project execution validation to prevent issues from one repo
	// being executed against a different project.
	SourceRepo string
	// MemberID is the team member ID for permission checks (GH-634).
	// When set and a TeamChecker is configured, the runner enforces RBAC before execution.
	MemberID string
	// Labels contains issue labels (e.g., "no-decompose", "pilot").
	// Flows from GitHub/Linear adapter → executor for decomposition decisions (GH-727).
	Labels []string
	// AcceptanceCriteria contains extracted acceptance criteria from the issue body (GH-920).
	// When present, included in the prompt and verified before commit.
	AcceptanceCriteria []string
	// FromPR is the PR number to resume session context from (GH-1267).
	// When set and UseFromPR is enabled, uses --from-pr <N> to resume the session
	// linked to the original PR, giving Claude full context of previous changes.
	// Typically set for autopilot-fix issues to continue from the failed PR's session.
	FromPR int
	// SourceAdapter identifies the adapter that originated this task (GH-1471).
	// Examples: "github", "linear", "jira", "gitlab", "azuredevops"
	// When non-empty and not "github", epic sub-issue creation uses the SubIssueCreator
	// interface instead of the gh CLI.
	SourceAdapter string
	// SourceIssueID is the issue identifier in the source adapter (GH-1471).
	// For GitHub: numeric issue number as string (e.g., "123")
	// For Linear: full identifier (e.g., "APP-456")
	// For Jira: issue key (e.g., "PROJ-789")
	// Used as parentID when creating sub-issues via SubIssueCreator.
	SourceIssueID string
	// LocalMode enables problem-solving prompt without PR constraints (GH-2103).
	// When true, BuildPrompt skips Navigator detection and uses a focused
	// problem-solving prompt suitable for local execution.
	LocalMode bool
	// State is the current issue state in the source adapter (GH-2867).
	// Examples: "open", "closed", "merged"
	State string
}

// QualityGateResult represents the result of a single quality gate check.
type QualityGateResult struct {
	// Name is the gate name (e.g., "build", "test", "lint")
	Name string
	// Passed indicates whether the gate passed
	Passed bool
	// Duration is how long the gate took to run
	Duration time.Duration
	// RetryCount is the number of retries attempted (0 if passed first try)
	RetryCount int
	// Error contains the error message if the gate failed
	Error string
}

// QualityGatesResult represents the aggregate quality gate results.
type QualityGatesResult struct {
	// Enabled indicates whether quality gates were configured and run
	Enabled bool
	// AllPassed indicates whether all gates passed
	AllPassed bool
	// Gates contains individual gate results
	Gates []QualityGateResult
	// TotalDuration is the total time spent running all gates
	TotalDuration time.Duration
	// TotalRetries is the sum of all retry attempts across gates
	TotalRetries int
}

// ExecutionResult represents the result of task execution by the Runner.
// It contains the execution outcome, any output or errors, and metrics
// about resource usage including token counts and estimated costs.
type ExecutionResult struct {
	// TaskID is the identifier of the executed task.
	TaskID string
	// Success indicates whether the task completed successfully.
	Success bool
	// Output contains the final output from Claude Code.
	Output string
	// Error contains error details if the execution failed.
	Error string
	// Duration is the total execution time.
	Duration time.Duration
	// PRUrl is the URL of the created pull request (if CreatePR was enabled).
	PRUrl string
	// CommitSHA is the git commit SHA of the last commit made during execution.
	CommitSHA string
	// TokensInput is the number of input tokens consumed.
	TokensInput int64
	// TokensOutput is the number of output tokens generated.
	TokensOutput int64
	// TokensTotal is the total token count (input + output).
	TokensTotal int64
	// CacheCreationInputTokens is the number of cache creation input tokens (GH-2164).
	CacheCreationInputTokens int64
	// CacheReadInputTokens is the number of cache read input tokens (GH-2164).
	CacheReadInputTokens int64
	// ResearchTokens is the number of tokens used by parallel research phase (GH-217).
	ResearchTokens int64
	// EstimatedCostUSD is the estimated cost in USD based on token usage.
	EstimatedCostUSD float64
	// FilesChanged is the number of files modified during execution.
	FilesChanged int
	// LinesAdded is the number of lines added across all changes.
	LinesAdded int
	// LinesRemoved is the number of lines removed across all changes.
	LinesRemoved int
	// ModelName is the Claude model used for execution.
	ModelName string
	// EffortLevel is the API effort level used (e.g., "low", "medium", "high"). GH-2807.
	EffortLevel string
	// ComplexityLevel is the detected task complexity tier (e.g., "trivial", "simple", "medium", "complex"). GH-2807.
	ComplexityLevel string
	// QualityGates contains the results of quality gate checks (if enabled)
	QualityGates *QualityGatesResult
	// IsEpic indicates this result is from epic planning (not execution)
	IsEpic bool
	// EpicPlan contains the planning result for epic tasks (GH-405)
	EpicPlan *EpicPlan
	// IntentWarning contains the reason if the intent judge flagged a mismatch.
	// When set, the PR was created despite intent misalignment (after retry failed).
	IntentWarning string
	// TitleRejected indicates the task failed at the conventional-commit title
	// guard and the runner has already posted a structured "how to fix" comment
	// (GH-2363). Callers should skip their generic failure-comment path.
	TitleRejected bool
	// Declined is true when Claude explicitly refused the task as unactionable,
	// emitting a DECLINED:<reason> marker in its response (GH-2777).
	// When true, callers should add pilot-needs-clarification instead of pilot-failed.
	Declined bool
	// DeclinedReason is the human-readable reason Claude provided for the decline.
	DeclinedReason string
	// Outcome is a fine-grained terminal classification ("declined", "no_op",
	// "no_commits", "stalled", "budget_exceeded") used by the dispatcher to pick
	// the persisted execution status instead of collapsing every !Success result
	// into "failed". Empty means "classify from Success/Declined/Error". TASK-358.
	Outcome string
	// PeakRSSMB is the peak subprocess RSS in MiB collected by the RSS sampler. GH-3028.
	// Zero on non-Linux/darwin platforms or when the sampler had no data.
	PeakRSSMB int
	// FinalRSSMB is the subprocess RSS at exit. GH-3028.
	FinalRSSMB int
}

// ProgressCallback is a function called during execution with progress updates.
// It receives the task ID, current phase name, progress percentage (0-100),
// and a human-readable message describing the current activity.
type ProgressCallback func(taskID string, phase string, progress int, message string)

// TokenCallback is a function called during execution with token usage updates.
// It receives the task ID, input tokens, output tokens, and the model name (may be empty
// before the first stream event arrives — callers should fall back to a default).
type TokenCallback func(taskID string, inputTokens, outputTokens int64, modelName string)

// TokenLimitCallback is called during execution with per-event token deltas.
// It returns true if execution should continue, false if the per-task token/duration
// limit has been exceeded and execution should be cancelled.
type TokenLimitCallback func(taskID string, deltaInput, deltaOutput int64) bool

// SubIssuePRCallback is called when a sub-issue PR is created during epic execution.
// Signature matches Controller.OnPRCreated so it can be wired directly.
type SubIssuePRCallback func(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string)

// SubIssueMergeWaitFn blocks until the given PR number is merged (or returns an error
// if the PR was closed, conflicted, or the wait timed out). Used by ExecuteSubIssues
// to enforce sequential ordering: sub-issue N+1 only starts after sub-issue N is merged.
type SubIssueMergeWaitFn func(ctx context.Context, prNumber int) error

// SubIssuePollerSkipFn is called with each newly-created GitHub sub-issue number so the
// poller marks it as already-processed and does not re-dispatch it (GH-3240).
type SubIssuePollerSkipFn func(issueNumber int)

// SubIssueCreator is an interface for creating sub-issues in external issue trackers.
// Adapters like Linear, Jira, GitLab, and Azure DevOps can implement this interface
// to allow epic decomposition to create sub-issues in the source tracker rather than GitHub.
type SubIssueCreator interface {
	// CreateIssue creates a new issue as a child of the given parent.
	// parentID: The parent issue identifier (e.g., "APP-123" for Linear, "PROJ-456" for Jira)
	// title: The issue title
	// body: The issue description/body
	// labels: Labels to apply to the new issue
	// Returns: identifier (e.g., "APP-124"), URL, error
	CreateIssue(ctx context.Context, parentID, title, body string, labels []string) (identifier string, url string, err error)
}

// PRCreator is an interface for creating pull/merge requests in external forges.
// Adapters like GitLab, Azure DevOps, etc. can implement this interface so the runner
// creates MRs via their native API instead of the gh CLI.
type PRCreator interface {
	// CreatePR creates a pull/merge request and returns its URL.
	CreatePR(ctx context.Context, sourceBranch, targetBranch, title, body string) (url string, err error)
}

// SubIssueLinker links a child issue to a parent issue using GitHub's native sub-issue API (GH-2211).
// *github.Client satisfies this interface via its LinkSubIssue method.
type SubIssueLinker interface {
	LinkSubIssue(ctx context.Context, owner, repo string, parentNum, childNum int) error
}
