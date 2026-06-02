package memory

import (
	"fmt"
	"strings"
)

// migrate creates necessary tables
func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS executions (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			project_path TEXT NOT NULL,
			status TEXT NOT NULL,
			output TEXT,
			error TEXT,
			duration_ms INTEGER,
			pr_url TEXT,
			commit_sha TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT,
			pattern_type TEXT NOT NULL,
			content TEXT NOT NULL,
			confidence REAL DEFAULT 1.0,
			uses INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			path TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			navigator_enabled BOOLEAN DEFAULT TRUE,
			last_active DATETIME DEFAULT CURRENT_TIMESTAMP,
			settings TEXT
		)`,
		// Cross-project pattern tables (TASK-11)
		`CREATE TABLE IF NOT EXISTS cross_patterns (
			id TEXT PRIMARY KEY,
			pattern_type TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			context TEXT,
			examples TEXT,
			confidence REAL DEFAULT 0.5,
			occurrences INTEGER DEFAULT 1,
			is_anti_pattern BOOLEAN DEFAULT FALSE,
			scope TEXT DEFAULT 'org',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS pattern_projects (
			pattern_id TEXT NOT NULL,
			project_path TEXT NOT NULL,
			uses INTEGER DEFAULT 1,
			success_count INTEGER DEFAULT 0,
			failure_count INTEGER DEFAULT 0,
			last_used DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (pattern_id, project_path),
			FOREIGN KEY (pattern_id) REFERENCES cross_patterns(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS pattern_feedback (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			project_path TEXT NOT NULL,
			outcome TEXT NOT NULL,
			confidence_delta REAL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (pattern_id) REFERENCES cross_patterns(id) ON DELETE CASCADE,
			FOREIGN KEY (execution_id) REFERENCES executions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_task ON executions(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_project ON executions(project_path)`,
		`CREATE INDEX IF NOT EXISTS idx_executions_created ON executions(created_at)`,
		// Metrics columns (TASK-13)
		`ALTER TABLE executions ADD COLUMN tokens_input INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN tokens_output INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN tokens_total INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN estimated_cost_usd REAL DEFAULT 0.0`,
		`ALTER TABLE executions ADD COLUMN files_changed INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN lines_added INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN lines_removed INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN model_name TEXT DEFAULT 'claude-sonnet-4-5'`,
		// Task queue columns for storing task details (GH-46)
		`ALTER TABLE executions ADD COLUMN task_title TEXT`,
		`ALTER TABLE executions ADD COLUMN task_description TEXT`,
		`ALTER TABLE executions ADD COLUMN task_branch TEXT`,
		`ALTER TABLE executions ADD COLUMN task_base_branch TEXT`,
		`ALTER TABLE executions ADD COLUMN task_create_pr BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE executions ADD COLUMN task_verbose BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE executions ADD COLUMN task_source_adapter TEXT DEFAULT ''`,
		`ALTER TABLE executions ADD COLUMN task_source_issue_id TEXT DEFAULT ''`,
		// GH-2326: persist Task.Labels across queue round-trip so no-decompose survives dispatch
		`ALTER TABLE executions ADD COLUMN task_labels TEXT DEFAULT ''`,
		// GH-2807: effort and complexity columns for cost-by-tier observability
		`ALTER TABLE executions ADD COLUMN effort_level TEXT`,
		`ALTER TABLE executions ADD COLUMN complexity_level TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_patterns_project ON patterns(project_path)`,
		// Cross-project pattern indexes
		`CREATE INDEX IF NOT EXISTS idx_cross_patterns_type ON cross_patterns(pattern_type)`,
		`CREATE INDEX IF NOT EXISTS idx_cross_patterns_scope ON cross_patterns(scope)`,
		`CREATE INDEX IF NOT EXISTS idx_cross_patterns_confidence ON cross_patterns(confidence DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cross_patterns_updated ON cross_patterns(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_cross_patterns_title ON cross_patterns(title)`,
		`CREATE INDEX IF NOT EXISTS idx_pattern_projects_project ON pattern_projects(project_path)`,
		`CREATE INDEX IF NOT EXISTS idx_pattern_feedback_pattern ON pattern_feedback(pattern_id)`,
		// Usage metering tables (TASK-16)
		`CREATE TABLE IF NOT EXISTS usage_events (
			id TEXT PRIMARY KEY,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			user_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			quantity INTEGER DEFAULT 0,
			unit_cost REAL DEFAULT 0.0,
			total_cost REAL DEFAULT 0.0,
			metadata TEXT,
			execution_id TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_user ON usage_events(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_project ON usage_events(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_timestamp ON usage_events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_type ON usage_events(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_execution ON usage_events(execution_id)`,
		// Dashboard sessions table (GH-367)
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			date TEXT NOT NULL,
			started_at DATETIME NOT NULL,
			ended_at DATETIME,
			total_input_tokens INTEGER DEFAULT 0,
			total_output_tokens INTEGER DEFAULT 0,
			total_cost_cents INTEGER DEFAULT 0,
			tasks_completed INTEGER DEFAULT 0,
			tasks_failed INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_date ON sessions(date)`,
		// Autopilot metrics snapshots (GH-728)
		`CREATE TABLE IF NOT EXISTS autopilot_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			snapshot_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			issues_success INTEGER DEFAULT 0,
			issues_failed INTEGER DEFAULT 0,
			issues_rate_limited INTEGER DEFAULT 0,
			prs_merged INTEGER DEFAULT 0,
			prs_failed INTEGER DEFAULT 0,
			prs_conflicting INTEGER DEFAULT 0,
			circuit_breaker_trips INTEGER DEFAULT 0,
			api_errors_total INTEGER DEFAULT 0,
			api_error_rate REAL DEFAULT 0.0,
			queue_depth INTEGER DEFAULT 0,
			failed_queue_depth INTEGER DEFAULT 0,
			active_prs INTEGER DEFAULT 0,
			success_rate REAL DEFAULT 0.0,
			avg_ci_wait_ms INTEGER DEFAULT 0,
			avg_merge_time_ms INTEGER DEFAULT 0,
			avg_execution_ms INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_autopilot_metrics_at ON autopilot_metrics(snapshot_at)`,
		// Brief history tracking (GH-1081)
		`CREATE TABLE IF NOT EXISTS brief_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sent_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			channel TEXT NOT NULL,
			brief_type TEXT NOT NULL DEFAULT 'daily',
			recipient TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_history_sent_at ON brief_history(sent_at)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_history_channel ON brief_history(channel)`,
		// Execution logs table (GH-1586)
		`CREATE TABLE IF NOT EXISTS execution_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			execution_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			level TEXT NOT NULL DEFAULT 'info',
			message TEXT NOT NULL,
			component TEXT DEFAULT 'executor'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_logs_timestamp ON execution_logs(timestamp)`,
		`CREATE TABLE IF NOT EXISTS model_outcomes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_type TEXT NOT NULL,
			model TEXT NOT NULL,
			outcome TEXT NOT NULL,
			tokens_used INTEGER DEFAULT 0,
			duration_ms INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_model_outcomes_task_model ON model_outcomes(task_type, model)`,
		`CREATE INDEX IF NOT EXISTS idx_model_outcomes_created ON model_outcomes(created_at)`,
		// Pattern performance tracking (GH-2020)
		`CREATE TABLE IF NOT EXISTS pattern_performance (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			task_type TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			success_count INTEGER DEFAULT 0,
			failure_count INTEGER DEFAULT 0,
			last_used DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(pattern_id, project_id, task_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pattern_performance_pattern ON pattern_performance(pattern_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pattern_performance_project ON pattern_performance(project_id)`,
		// Pending approval requests awaiting human decision (GH-2657)
		`CREATE TABLE IF NOT EXISTS approval_pending (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			stage TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			metadata TEXT DEFAULT '',
			approvers TEXT DEFAULT '',
			preferred_channel TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_approval_pending_expires ON approval_pending(expires_at)`,
		// Approval decision columns on executions (GH-2667)
		`ALTER TABLE executions ADD COLUMN approval_request_id TEXT DEFAULT ''`,
		`ALTER TABLE executions ADD COLUMN approval_decision TEXT DEFAULT ''`,
		`ALTER TABLE executions ADD COLUMN approval_decision_at DATETIME`,
		`ALTER TABLE executions ADD COLUMN approval_decision_by TEXT DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_executions_approval_request ON executions(approval_request_id)`,
		// Per-model token/cost/execution counters on autopilot_metrics (GH-2856)
		`ALTER TABLE autopilot_metrics ADD COLUMN tokens_consumed_json TEXT DEFAULT '{}'`,
		`ALTER TABLE autopilot_metrics ADD COLUMN execution_cost_usd_json TEXT DEFAULT '{}'`,
		`ALTER TABLE autopilot_metrics ADD COLUMN executions_by_result_json TEXT DEFAULT '{}'`,
		// GH-3028: RSS telemetry — peak and final resident set size for subprocess OOM diagnostics.
		`ALTER TABLE executions ADD COLUMN peak_rss_mb INTEGER DEFAULT 0`,
		`ALTER TABLE executions ADD COLUMN final_rss_mb INTEGER DEFAULT 0`,
	}

	for _, migration := range migrations {
		_, err := s.db.Exec(migration)
		if err != nil {
			// Ignore "duplicate column" errors from ALTER TABLE migrations
			// SQLite returns "duplicate column name" when column already exists
			errStr := err.Error()
			if strings.Contains(errStr, "duplicate column") {
				continue
			}
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// TASK-358: correct historically-misclassified outcomes (declined/no-op/stalled
	// that were collapsed into status='failed' before the dispatcher classified them).
	if err := s.reclassifyLegacyOutcomes(); err != nil {
		return fmt.Errorf("reclassify legacy outcomes: %w", err)
	}

	return nil
}

// reclassifyLegacyOutcomes corrects executions that the dispatcher previously
// recorded as status='failed' when they were actually non-failure terminal
// outcomes — no-op (work already on base / no edits), rate-limited, skipped
// (never ran / cancelled), stalled/budget, or infra/plumbing (resource kill,
// push/PR/worktree/branch). Before TASK-358 every !Success result collapsed into
// "failed", inflating the dashboard's QUEUE "failed" count.
//
// Each UPDATE is guarded by status='failed' and the statements run in the same
// precedence order as TerminalStatus (no-op first, infra last) so a row matching
// more than one signature lands in the most "this isn't a failure" bucket.
// Classification uses the deterministic error signatures the runner writes, so it
// only touches rows it can positively identify; genuine failures (quality gates,
// planning, unknown exit-1) carry none of these signatures and are left as
// "failed". Idempotent: after the first pass no 'failed' row matches, so running
// on every startup is a cheap, indexed no-op. Declined rows cannot be recovered
// here because the decline reason was never persisted to executions.error.
//
// Keep the LIKE patterns in sync with the signature lists in executor/runner.go.
func (s *Store) reclassifyLegacyOutcomes() error {
	stmts := []string{
		`UPDATE executions SET status = 'no_op'
		 WHERE status = 'failed' AND (
			error LIKE '%no new commit produced%' OR
			error LIKE '%no commits relative to base%' OR
			error LIKE '%no_changes%' OR
			error LIKE '%made no code changes%'
		 )`,
		`UPDATE executions SET status = 'rate_limited'
		 WHERE status = 'failed' AND (
			error LIKE '%hit your limit%' OR
			error LIKE '%rate limit%' OR
			error LIKE '%usage limit%'
		 )`,
		`UPDATE executions SET status = 'skipped'
		 WHERE status = 'failed' AND (
			error LIKE '%stale queued task recovered%' OR
			error LIKE '%context canceled%' OR
			error LIKE '%context cancelled%'
		 )`,
		`UPDATE executions SET status = 'stalled'
		 WHERE status = 'failed' AND (
			error LIKE '%session stalled%' OR
			error LIKE '%budget limit exceeded%'
		 )`,
		`UPDATE executions SET status = 'infra'
		 WHERE status = 'failed' AND (
			error LIKE '%oom_killed%' OR
			error LIKE '%exit code 137%' OR
			error LIKE '%SIGKILL%' OR
			error LIKE '%signal: killed%' OR
			error LIKE '%push failed%' OR
			error LIKE '%PR creation failed%' OR
			error LIKE '%worktree creation failed%' OR
			error LIKE '%create/switch branch%' OR
			error LIKE '%branch switch failed%'
		 )`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
