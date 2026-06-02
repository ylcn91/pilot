package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Execution represents a task execution record stored in the database.
// It captures the complete execution history including status, output, metrics, and PR information.
type Execution struct {
	ID          string
	TaskID      string
	ProjectPath string
	// UserID identifies the user/tenant that owns this execution.
	// Empty in single-tenant deployments; populated when multi-user mode is enabled.
	// Used as the pivot for `usage_events` aggregation (GH-2429).
	UserID      string
	Status      string
	Output      string
	Error       string
	DurationMs  int64
	PRUrl       string
	CommitSHA   string
	CreatedAt   time.Time
	CompletedAt *time.Time
	// Metrics fields (TASK-13)
	TokensInput      int64
	TokensOutput     int64
	TokensTotal      int64
	EstimatedCostUSD float64
	FilesChanged     int
	LinesAdded       int
	LinesRemoved     int
	ModelName        string
	// GH-2807: effort and complexity for cost-by-tier observability
	EffortLevel     string `json:"effort_level,omitempty"`
	ComplexityLevel string `json:"complexity_level,omitempty"`
	// Task queue fields (GH-46) - store task details for deferred execution
	TaskTitle         string
	TaskDescription   string
	TaskBranch        string
	TaskBaseBranch    string
	TaskCreatePR      bool
	TaskVerbose       bool
	TaskSourceAdapter string // Source adapter (e.g., "github", "gitlab", "linear")
	TaskSourceIssueID string // Issue ID in the source adapter
	// GH-2326: persisted Task.Labels so label-driven gates (no-decompose, autopilot-fix, etc.)
	// survive the dispatcher queue → worker round-trip.
	TaskLabels []string
	// Approval decision fields (GH-2667)
	ApprovalRequestID  string
	ApprovalDecision   string
	ApprovalDecisionAt *time.Time
	ApprovalDecisionBy string
	// GH-3028: RSS telemetry
	PeakRSSMB  int
	FinalRSSMB int
}

// SaveExecution saves an execution record to the database.
// The execution ID must be unique; duplicate IDs will cause an error.
func (s *Store) SaveExecution(exec *Execution) error {
	labelsJSON, err := marshalLabels(exec.TaskLabels)
	if err != nil {
		return fmt.Errorf("failed to marshal task labels: %w", err)
	}
	return s.withRetry("SaveExecution", func() error {
		_, err := s.db.Exec(`
			INSERT INTO executions (id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, completed_at,
				tokens_input, tokens_output, tokens_total, estimated_cost_usd, files_changed, lines_added, lines_removed, model_name,
				task_title, task_description, task_branch, task_base_branch, task_create_pr, task_verbose,
				task_source_adapter, task_source_issue_id, task_labels,
				approval_request_id, effort_level, complexity_level)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, exec.ID, exec.TaskID, exec.ProjectPath, exec.Status, exec.Output, exec.Error, exec.DurationMs, exec.PRUrl, exec.CommitSHA, exec.CompletedAt,
			exec.TokensInput, exec.TokensOutput, exec.TokensTotal, exec.EstimatedCostUSD, exec.FilesChanged, exec.LinesAdded, exec.LinesRemoved, exec.ModelName,
			exec.TaskTitle, exec.TaskDescription, exec.TaskBranch, exec.TaskBaseBranch, exec.TaskCreatePR, exec.TaskVerbose,
			exec.TaskSourceAdapter, exec.TaskSourceIssueID, labelsJSON,
			exec.ApprovalRequestID, exec.EffortLevel, exec.ComplexityLevel)
		return err
	})
}

// marshalLabels serializes labels to JSON; returns "" when the slice is empty
// so the DB column stays compatible with pre-migration rows and default "".
func marshalLabels(labels []string) (string, error) {
	if len(labels) == 0 {
		return "", nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalLabels parses JSON-encoded labels; empty/whitespace → nil slice.
func unmarshalLabels(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var labels []string
	if err := json.Unmarshal([]byte(s), &labels); err != nil {
		// Legacy / malformed rows: return nil rather than failing the read.
		return nil
	}
	return labels
}

// GetExecution retrieves an execution by its unique ID.
// Returns sql.ErrNoRows if the execution is not found.
func (s *Store) GetExecution(id string) (*Execution, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, project_path, status, output, error, duration_ms, pr_url, commit_sha, created_at, completed_at,
			COALESCE(tokens_input, 0), COALESCE(tokens_output, 0), COALESCE(tokens_total, 0),
			COALESCE(estimated_cost_usd, 0), COALESCE(files_changed, 0), COALESCE(lines_added, 0),
			COALESCE(lines_removed, 0), COALESCE(model_name, ''),
			COALESCE(task_title, ''), COALESCE(task_description, ''), COALESCE(task_branch, ''),
			COALESCE(task_base_branch, ''), COALESCE(task_create_pr, 0), COALESCE(task_verbose, 0),
			COALESCE(task_source_adapter, ''), COALESCE(task_source_issue_id, ''),
			COALESCE(task_labels, ''),
			COALESCE(approval_request_id, ''), COALESCE(approval_decision, ''),
			approval_decision_at,
			COALESCE(approval_decision_by, ''),
			COALESCE(effort_level, ''), COALESCE(complexity_level, '')
		FROM executions WHERE id = ?
	`, id)

	var exec Execution
	var completedAt sql.NullTime
	var approvalDecisionAt sql.NullTime
	var labelsJSON string
	err := row.Scan(&exec.ID, &exec.TaskID, &exec.ProjectPath, &exec.Status, &exec.Output, &exec.Error, &exec.DurationMs, &exec.PRUrl, &exec.CommitSHA, &exec.CreatedAt, &completedAt,
		&exec.TokensInput, &exec.TokensOutput, &exec.TokensTotal, &exec.EstimatedCostUSD, &exec.FilesChanged, &exec.LinesAdded, &exec.LinesRemoved, &exec.ModelName,
		&exec.TaskTitle, &exec.TaskDescription, &exec.TaskBranch, &exec.TaskBaseBranch, &exec.TaskCreatePR, &exec.TaskVerbose,
		&exec.TaskSourceAdapter, &exec.TaskSourceIssueID, &labelsJSON,
		&exec.ApprovalRequestID, &exec.ApprovalDecision, &approvalDecisionAt, &exec.ApprovalDecisionBy,
		&exec.EffortLevel, &exec.ComplexityLevel)
	if err != nil {
		return nil, err
	}
	exec.TaskLabels = unmarshalLabels(labelsJSON)
	if approvalDecisionAt.Valid {
		exec.ApprovalDecisionAt = &approvalDecisionAt.Time
	}

	if completedAt.Valid {
		exec.CompletedAt = &completedAt.Time
	}

	return &exec, nil
}

// HasCompletedExecution checks whether a genuine completed execution exists for the given task
// and project. "Genuine" means: status=completed, no error, AND at least one deliverable
// (commit_sha or pr_url is set). This mirrors IsTaskShipped in the executor package.
//
// Rows excluded from the count:
//   - status != "completed" (still running/queued/failed)
//   - non-empty error field (orphan recovery, GH-2315)
//   - no commit_sha AND no pr_url (epic-parent rows that produced no real work, TASK-296)
//
// The cross-site invariant — HasCompletedExecution and IsTaskShipped always agree — is enforced
// by internal/integration/task_completion_invariant_test.go.
func (s *Store) HasCompletedExecution(taskID, projectPath string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM executions
		WHERE task_id = ? AND project_path = ? AND status = 'completed'
			AND (error IS NULL OR error = '')
			AND (commit_sha != '' OR pr_url != '')
	`, taskID, projectPath).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// InvalidateCompletion deletes genuine completed execution records for the given task and
// project, allowing re-dispatch. Targets only rows that HasCompletedExecution would count
// (status='completed', no error, at least one deliverable), leaving orphan-recovered rows
// and epic-parent no-deliverable rows untouched.
func (s *Store) InvalidateCompletion(taskID, projectPath string) error {
	_, err := s.db.Exec(`
		DELETE FROM executions
		WHERE task_id = ? AND project_path = ? AND status = 'completed'
			AND (error IS NULL OR error = '')
			AND (commit_sha != '' OR pr_url != '')
	`, taskID, projectPath)
	if err != nil {
		return fmt.Errorf("invalidate completion for %s at %s: %w", taskID, projectPath, err)
	}
	return nil
}

// DeleteExecution removes an execution row by ID. Used to clean up orphan rows
// when the same task already has a completed execution.
func (s *Store) DeleteExecution(id string) error {
	_, err := s.db.Exec("DELETE FROM executions WHERE id = ?", id)
	return err
}

// IsTaskQueued checks if a task with the given ID is already queued or running.
// Used to prevent duplicate task submissions.
func (s *Store) IsTaskQueued(taskID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM executions
		WHERE task_id = ? AND status IN ('queued', 'pending', 'running')
	`, taskID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
