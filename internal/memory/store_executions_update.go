package memory

import (
	"context"
	"database/sql"
	"fmt"
)

// SetApprovalDecision records an approval decision on the execution linked to requestID.
// It sets approval_decision, approval_decision_at, and approval_decision_by on the row
// whose approval_request_id matches. Returns sql.ErrNoRows if no matching row is found.
func (s *Store) SetApprovalDecision(ctx context.Context, requestID string, decision string, by string) error {
	if requestID == "" {
		return sql.ErrNoRows
	}
	return s.withRetry("SetApprovalDecision", func() error {
		result, err := s.db.ExecContext(ctx, `
			UPDATE executions
			SET approval_decision    = ?,
			    approval_decision_at = CURRENT_TIMESTAMP,
			    approval_decision_by = ?
			WHERE approval_request_id = ?
		`, decision, by, requestID)
		if err != nil {
			return fmt.Errorf("SetApprovalDecision: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("SetApprovalDecision rows affected: %w", err)
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// SetApprovalRequestID records the approval request ID on the most-recent execution
// row for the given task. Must be called after SubmitApprovalRequest succeeds so
// that SetApprovalDecision's WHERE clause can later match the row.
// Returns sql.ErrNoRows when no execution row exists for taskID yet.
func (s *Store) SetApprovalRequestID(ctx context.Context, taskID, requestID string) error {
	if taskID == "" || requestID == "" {
		return nil
	}
	return s.withRetry("SetApprovalRequestID", func() error {
		result, err := s.db.ExecContext(ctx, `
			UPDATE executions
			SET approval_request_id = ?
			WHERE id = (
				SELECT id FROM executions
				WHERE task_id = ?
				ORDER BY created_at DESC
				LIMIT 1
			)
		`, requestID, taskID)
		if err != nil {
			return fmt.Errorf("SetApprovalRequestID: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("SetApprovalRequestID rows affected: %w", err)
		}
		if rows == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// UpdateExecutionStatus updates the status of an execution record.
// Optionally sets the error message if provided. Also sets completed_at for terminal states.
func (s *Store) UpdateExecutionStatus(id, status string, errorMsg ...string) error {
	var errStr *string
	if len(errorMsg) > 0 && errorMsg[0] != "" {
		errStr = &errorMsg[0]
	}

	// Set completed_at for terminal states
	if status == "completed" || status == "failed" || status == "cancelled" || status == "declined" || status == "stalled" || status == "no_op" || status == "rate_limited" || status == "infra" || status == "skipped" {
		return s.withRetry("UpdateExecutionStatus", func() error {
			_, err := s.db.Exec(`
				UPDATE executions
				SET status = ?, error = COALESCE(?, error), completed_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`, status, errStr, id)
			return err
		})
	}

	return s.withRetry("UpdateExecutionStatus", func() error {
		_, err := s.db.Exec(`
			UPDATE executions
			SET status = ?, error = COALESCE(?, error)
			WHERE id = ?
		`, status, errStr, id)
		return err
	})
}

// UpdateExecutionStatusByTaskID updates the status of the most recent execution
// for a given task ID and project path. Used by autopilot to mark failed
// executions as completed when the PR is merged externally.
// The projectPath scope prevents cross-project clobbering when the same task ID
// appears in multiple repos.
//
// TASK-358: the source scope is the non-success set ('failed', 'no_op', 'stalled')
// rather than 'failed' alone, so an execution the dispatcher now classifies as a
// no-op/stalled outcome still heals to the merged status when its PR lands.
func (s *Store) UpdateExecutionStatusByTaskID(taskID, projectPath, status string) error {
	return s.withRetry("UpdateExecutionStatusByTaskID", func() error {
		_, err := s.db.Exec(`
			UPDATE executions
			SET status = ?, completed_at = CURRENT_TIMESTAMP
			WHERE task_id = ? AND project_path = ? AND status IN ('failed', 'no_op', 'stalled', 'rate_limited', 'infra', 'skipped')
		`, status, taskID, projectPath)
		return err
	})
}

// SelfHealExecutionAfterMerge promotes any non-success row ("failed", "no_op",
// "stalled" — TASK-358) for the given task ID (scoped to projectPath) to
// "completed" and stamps the PR URL so the dashboard reflects the merged outcome.
// Used when autopilot observes a merge for an issue whose previous execution row
// was recorded as a non-success (e.g. user-pushed commits, sub-issue shipped via
// parent epic, or a phantom no-op whose work was already on base). GH-2402.
//
// projectPath MUST be the same value the executor stored in executions.project_path
// — an absolute filesystem path (e.g. /Users/me/proj), NOT an owner/repo slug. The
// scope prevents cross-project clobbering when the same task ID (GH-N is only unique
// per repo) appears in multiple repos. When projectPath is empty the scope is
// dropped and rows match by task_id alone (legacy single-repo behavior); this also
// guards against a caller passing the wrong discriminator silently healing nothing.
// TASK-352.
func (s *Store) SelfHealExecutionAfterMerge(taskID, projectPath, prURL string) error {
	return s.withRetry("SelfHealExecutionAfterMerge", func() error {
		_, err := s.db.Exec(`
			UPDATE executions
			SET status = 'completed',
				completed_at = CURRENT_TIMESTAMP,
				pr_url = CASE WHEN ? <> '' THEN ? ELSE pr_url END
			WHERE task_id = ? AND status IN ('failed', 'no_op', 'stalled', 'rate_limited', 'infra', 'skipped') AND (? = '' OR project_path = ?)
		`, prURL, prURL, taskID, projectPath, projectPath)
		return err
	})
}

// UpdateExecutionResult updates the result fields of an execution record.
// Called when task execution completes successfully with PR/commit info.
func (s *Store) UpdateExecutionResult(id string, prURL, commitSHA string, durationMs int64) error {
	return s.withRetry("UpdateExecutionResult", func() error {
		_, err := s.db.Exec(`
			UPDATE executions
			SET pr_url = ?, commit_sha = ?, duration_ms = ?
			WHERE id = ?
		`, prURL, commitSHA, durationMs, id)
		return err
	})
}

// UpdateExecutionEffort records the resolved effort and complexity levels for a completed execution.
// Called after execution finishes so cost-by-tier queries can group rows by tier.
func (s *Store) UpdateExecutionEffort(id, effortLevel, complexityLevel string) error {
	return s.withRetry("UpdateExecutionEffort", func() error {
		_, err := s.db.Exec(`
			UPDATE executions
			SET effort_level = ?, complexity_level = ?
			WHERE id = ?
		`, effortLevel, complexityLevel, id)
		return err
	})
}
