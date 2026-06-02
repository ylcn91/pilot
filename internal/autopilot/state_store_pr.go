package autopilot

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SavePRState persists a PR state to the database (upsert).
func (s *StateStore) SavePRState(pr *PRState) error {
	_, err := s.db.Exec(`
		INSERT INTO autopilot_pr_state (
			pr_number, pr_url, issue_number, branch_name, head_sha,
			stage, ci_status, last_checked, ci_wait_started_at,
			merge_attempts, error, created_at, updated_at,
			release_version, release_bump_type, merge_notification_posted,
			approval_request_id, approval_decision, approval_requested_at,
			post_merge_sha, post_merge_ci_started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pr_number) DO UPDATE SET
			pr_url = excluded.pr_url,
			issue_number = excluded.issue_number,
			branch_name = excluded.branch_name,
			head_sha = excluded.head_sha,
			stage = excluded.stage,
			ci_status = excluded.ci_status,
			last_checked = excluded.last_checked,
			ci_wait_started_at = excluded.ci_wait_started_at,
			merge_attempts = excluded.merge_attempts,
			error = excluded.error,
			updated_at = CURRENT_TIMESTAMP,
			release_version = excluded.release_version,
			release_bump_type = excluded.release_bump_type,
			merge_notification_posted = excluded.merge_notification_posted,
			approval_request_id = excluded.approval_request_id,
			approval_decision = excluded.approval_decision,
			approval_requested_at = excluded.approval_requested_at,
			post_merge_sha = excluded.post_merge_sha,
			post_merge_ci_started_at = excluded.post_merge_ci_started_at
	`,
		pr.PRNumber, pr.PRURL, pr.IssueNumber, pr.BranchName, pr.HeadSHA,
		string(pr.Stage), string(pr.CIStatus),
		nullTime(pr.LastChecked), nullTime(pr.CIWaitStartedAt),
		pr.MergeAttempts, pr.Error, nullTime(pr.CreatedAt),
		pr.ReleaseVersion, string(pr.ReleaseBumpType), pr.MergeNotificationPosted,
		pr.ApprovalRequestID, pr.ApprovalDecision, nullTime(pr.ApprovalRequestedAt),
		pr.PostMergeSHA, nullTime(pr.PostMergeCIStartedAt),
	)
	return err
}

// GetPRState retrieves a single PR state by number.
// Returns nil, nil if not found.
func (s *StateStore) GetPRState(prNumber int) (*PRState, error) {
	row := s.db.QueryRow(`
		SELECT pr_number, pr_url, issue_number, branch_name, head_sha,
			stage, ci_status, last_checked, ci_wait_started_at,
			merge_attempts, error, created_at,
			release_version, release_bump_type, merge_notification_posted,
			approval_request_id, approval_decision, approval_requested_at,
			post_merge_sha, post_merge_ci_started_at
		FROM autopilot_pr_state WHERE pr_number = ?
	`, prNumber)

	pr, err := scanPRState(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pr, nil
}

// LoadAllPRStates retrieves all persisted PR states.
func (s *StateStore) LoadAllPRStates() ([]*PRState, error) {
	rows, err := s.db.Query(`
		SELECT pr_number, pr_url, issue_number, branch_name, head_sha,
			stage, ci_status, last_checked, ci_wait_started_at,
			merge_attempts, error, created_at,
			release_version, release_bump_type, merge_notification_posted,
			approval_request_id, approval_decision, approval_requested_at,
			post_merge_sha, post_merge_ci_started_at
		FROM autopilot_pr_state
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var states []*PRState
	for rows.Next() {
		var pr PRState
		var lastChecked, ciWaitStartedAt, createdAt, approvalRequestedAt, postMergeCIStartedAt sql.NullTime
		var stage, ciStatus, relBumpType string

		if err := rows.Scan(
			&pr.PRNumber, &pr.PRURL, &pr.IssueNumber, &pr.BranchName, &pr.HeadSHA,
			&stage, &ciStatus, &lastChecked, &ciWaitStartedAt,
			&pr.MergeAttempts, &pr.Error, &createdAt,
			&pr.ReleaseVersion, &relBumpType, &pr.MergeNotificationPosted,
			&pr.ApprovalRequestID, &pr.ApprovalDecision, &approvalRequestedAt,
			&pr.PostMergeSHA, &postMergeCIStartedAt,
		); err != nil {
			return nil, err
		}

		pr.Stage = PRStage(stage)
		pr.CIStatus = CIStatus(ciStatus)
		pr.ReleaseBumpType = BumpType(relBumpType)
		if lastChecked.Valid {
			pr.LastChecked = lastChecked.Time
		}
		if ciWaitStartedAt.Valid {
			pr.CIWaitStartedAt = ciWaitStartedAt.Time
		}
		if createdAt.Valid {
			pr.CreatedAt = createdAt.Time
		}
		if approvalRequestedAt.Valid {
			pr.ApprovalRequestedAt = approvalRequestedAt.Time
		}
		if postMergeCIStartedAt.Valid {
			pr.PostMergeCIStartedAt = postMergeCIStartedAt.Time
		}
		states = append(states, &pr)
	}
	return states, nil
}

// RemovePRState deletes a PR state record.
func (s *StateStore) RemovePRState(prNumber int) error {
	_, err := s.db.Exec(`DELETE FROM autopilot_pr_state WHERE pr_number = ?`, prNumber)
	return err
}

// releasingStaleThreshold bounds how long a PR row may sit at stage='releasing'
// before it is treated as wedged. 'releasing' is not a terminal stage, but a row
// stuck past this threshold indicates a release that never completed (B4/TASK-309).
// Shared by PurgeTerminalPRStates (B4 housekeeping purge) and the scanner skip
// gate (B3, PersistedReleasingAge) so both agree on what "stale" means.
const releasingStaleThreshold = 30 * time.Minute

// PurgeTerminalPRStates removes housekeeping-eligible PR state rows: terminal
// 'failed' rows older than olderThan, plus 'releasing' rows untouched for longer
// than releasingStaleThreshold. A 'releasing' row is not strictly terminal, but
// one stuck past the threshold is a wedged release (B4/TASK-309) — purging it is a
// safety net so the row cannot live forever and suppress re-discovery by
// ScanRecentlyMergedPRs. Active PRs (fresh rows, other stages) are never purged.
func (s *StateStore) PurgeTerminalPRStates(olderThan time.Duration) (int64, error) {
	// updated_at is written as CURRENT_TIMESTAMP (SQLite UTC), so the cutoffs must
	// be evaluated against SQLite's own UTC clock — binding a Go (local) time.Time
	// here mis-compares by the host's tz offset. <= keeps the olderThan=0 degenerate
	// case ("purge all terminal rows now") reaping same-second rows.
	result, err := s.db.Exec(`
		DELETE FROM autopilot_pr_state
		WHERE (stage = 'failed'    AND updated_at <= datetime('now', ?))
		   OR (stage = 'releasing' AND updated_at <= datetime('now', ?))
	`,
		fmt.Sprintf("-%d seconds", int64(olderThan.Seconds())),
		fmt.Sprintf("-%d seconds", int64(releasingStaleThreshold.Seconds())),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// PersistedReleasingAge reports the age (time since last update) of a persisted PR
// row at stage='releasing'. found is false when no row exists for prNumber or the
// row is in a different stage. The scanner uses this (B3/TASK-309) to skip
// re-registering a release that is already in flight in the state store but absent
// from the in-memory activePRs map (e.g. after a daemon restart), without relying
// on the in-memory map alone. Returning the age (rather than a bool) lets the
// caller ignore genuinely wedged rows so they can be re-driven.
func (s *StateStore) PersistedReleasingAge(prNumber int) (age time.Duration, found bool, err error) {
	var stage string
	var updatedAt sql.NullTime
	row := s.db.QueryRow(`SELECT stage, updated_at FROM autopilot_pr_state WHERE pr_number = ?`, prNumber)
	if scanErr := row.Scan(&stage, &updatedAt); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, scanErr
	}
	if PRStage(stage) != StageReleasing {
		return 0, false, nil
	}
	if !updatedAt.Valid {
		// Row exists at 'releasing' but has no timestamp (should not happen given
		// the CURRENT_TIMESTAMP default); treat as wedged so it is not skipped.
		return releasingStaleThreshold, true, nil
	}
	return time.Since(updatedAt.Time), true, nil
}

// scanPRState scans a single row into a PRState.
func scanPRState(row *sql.Row) (*PRState, error) {
	var pr PRState
	var lastChecked, ciWaitStartedAt, createdAt, approvalRequestedAt, postMergeCIStartedAt sql.NullTime
	var stage, ciStatus, relBumpType string

	err := row.Scan(
		&pr.PRNumber, &pr.PRURL, &pr.IssueNumber, &pr.BranchName, &pr.HeadSHA,
		&stage, &ciStatus, &lastChecked, &ciWaitStartedAt,
		&pr.MergeAttempts, &pr.Error, &createdAt,
		&pr.ReleaseVersion, &relBumpType, &pr.MergeNotificationPosted,
		&pr.ApprovalRequestID, &pr.ApprovalDecision, &approvalRequestedAt,
		&pr.PostMergeSHA, &postMergeCIStartedAt,
	)
	if err != nil {
		return nil, err
	}

	pr.Stage = PRStage(stage)
	pr.CIStatus = CIStatus(ciStatus)
	pr.ReleaseBumpType = BumpType(relBumpType)
	if lastChecked.Valid {
		pr.LastChecked = lastChecked.Time
	}
	if ciWaitStartedAt.Valid {
		pr.CIWaitStartedAt = ciWaitStartedAt.Time
	}
	if createdAt.Valid {
		pr.CreatedAt = createdAt.Time
	}
	if approvalRequestedAt.Valid {
		pr.ApprovalRequestedAt = approvalRequestedAt.Time
	}
	if postMergeCIStartedAt.Valid {
		pr.PostMergeCIStartedAt = postMergeCIStartedAt.Time
	}
	return &pr, nil
}

// nullTime converts a time.Time to sql.NullTime, treating zero time as NULL.
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}
