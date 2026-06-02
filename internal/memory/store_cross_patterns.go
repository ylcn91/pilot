package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// CrossPattern represents a pattern that applies across multiple projects.
// It enables knowledge sharing between projects within an organization,
// tracking confidence based on usage outcomes.
type CrossPattern struct {
	ID            string
	Type          string
	Title         string
	Description   string
	Context       string
	Examples      []string
	Confidence    float64
	Occurrences   int
	IsAntiPattern bool
	Scope         string // "project", "org", "global"
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PatternProjectLink represents the relationship between a cross-project pattern and a specific project.
// It tracks usage statistics and success/failure counts for the pattern within that project.
type PatternProjectLink struct {
	PatternID    string
	ProjectPath  string
	Uses         int
	SuccessCount int
	FailureCount int
	LastUsed     time.Time
}

// PatternFeedback records the outcome when a pattern was applied during an execution.
// It is used to adjust pattern confidence based on real-world results.
type PatternFeedback struct {
	ID              int64
	PatternID       string
	ExecutionID     string
	ProjectPath     string
	Outcome         string // "success", "failure", "neutral"
	ConfidenceDelta float64
	CreatedAt       time.Time
}

// SaveCrossPattern saves a new cross-project pattern or updates an existing one.
// On conflict, the pattern is updated and its occurrence count is incremented.
func (s *Store) SaveCrossPattern(pattern *CrossPattern) error {
	examples, _ := json.Marshal(pattern.Examples)

	return s.withRetry("SaveCrossPattern", func() error {
		_, err := s.db.Exec(`
			INSERT INTO cross_patterns (id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(id) DO UPDATE SET
				title = excluded.title,
				description = excluded.description,
				context = excluded.context,
				examples = excluded.examples,
				confidence = excluded.confidence,
				occurrences = cross_patterns.occurrences + 1,
				updated_at = CURRENT_TIMESTAMP
		`, pattern.ID, pattern.Type, pattern.Title, pattern.Description, pattern.Context, string(examples), pattern.Confidence, pattern.Occurrences, pattern.IsAntiPattern, pattern.Scope)
		return err
	})
}

// GetCrossPattern retrieves a cross-project pattern by its unique ID.
// Returns sql.ErrNoRows if the pattern is not found.
func (s *Store) GetCrossPattern(id string) (*CrossPattern, error) {
	row := s.db.QueryRow(`
		SELECT id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope, created_at, updated_at
		FROM cross_patterns WHERE id = ?
	`, id)

	var p CrossPattern
	var examplesStr string
	if err := row.Scan(&p.ID, &p.Type, &p.Title, &p.Description, &p.Context, &examplesStr, &p.Confidence, &p.Occurrences, &p.IsAntiPattern, &p.Scope, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}

	if examplesStr != "" {
		if err := json.Unmarshal([]byte(examplesStr), &p.Examples); err != nil {
			slog.Warn("failed to unmarshal cross pattern examples",
				slog.String("pattern_id", p.ID),
				slog.Any("error", err))
		}
	}

	return &p, nil
}

// GetCrossPatternsByType retrieves all cross-project patterns of a specific type.
// Results are ordered by confidence and occurrence count descending.
func (s *Store) GetCrossPatternsByType(patternType string) ([]*CrossPattern, error) {
	rows, err := s.db.Query(`
		SELECT id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope, created_at, updated_at
		FROM cross_patterns
		WHERE pattern_type = ?
		ORDER BY confidence DESC, occurrences DESC
	`, patternType)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanCrossPatterns(rows)
}

// GetCrossPatternsForProject retrieves cross-project patterns relevant to a specific project.
// This includes patterns directly linked to the project and organization-scoped patterns.
// If includeGlobal is true, globally-scoped patterns are also included.
func (s *Store) GetCrossPatternsForProject(projectPath string, includeGlobal bool) ([]*CrossPattern, error) {
	query := `
		SELECT DISTINCT cp.id, cp.pattern_type, cp.title, cp.description, cp.context, cp.examples,
		       cp.confidence, cp.occurrences, cp.is_anti_pattern, cp.scope, cp.created_at, cp.updated_at
		FROM cross_patterns cp
		LEFT JOIN pattern_projects pp ON cp.id = pp.pattern_id
		WHERE pp.project_path = ?
		   OR cp.scope = 'org'
	`
	if includeGlobal {
		query += ` OR cp.scope = 'global'`
	}
	query += ` ORDER BY cp.confidence DESC, cp.occurrences DESC`

	rows, err := s.db.Query(query, projectPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanCrossPatterns(rows)
}

// GetTopCrossPatterns retrieves the highest-confidence cross-project patterns.
// Only patterns with confidence at or above minConfidence are returned, up to the specified limit.
func (s *Store) GetTopCrossPatterns(limit int, minConfidence float64) ([]*CrossPattern, error) {
	rows, err := s.db.Query(`
		SELECT id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope, created_at, updated_at
		FROM cross_patterns
		WHERE confidence >= ?
		ORDER BY confidence DESC, occurrences DESC
		LIMIT ?
	`, minConfidence, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanCrossPatterns(rows)
}

// scanCrossPatterns scans rows into CrossPattern slice
func (s *Store) scanCrossPatterns(rows *sql.Rows) ([]*CrossPattern, error) {
	var patterns []*CrossPattern
	for rows.Next() {
		var p CrossPattern
		var examplesStr string
		if err := rows.Scan(&p.ID, &p.Type, &p.Title, &p.Description, &p.Context, &examplesStr, &p.Confidence, &p.Occurrences, &p.IsAntiPattern, &p.Scope, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if examplesStr != "" {
			if err := json.Unmarshal([]byte(examplesStr), &p.Examples); err != nil {
				slog.Warn("failed to unmarshal cross pattern examples",
					slog.String("pattern_id", p.ID),
					slog.Any("error", err))
			}
		}
		patterns = append(patterns, &p)
	}
	return patterns, rows.Err()
}

// LinkPatternToProject creates or updates a relationship between a pattern and a project.
// If the link exists, the usage count is incremented; otherwise a new link is created.
func (s *Store) LinkPatternToProject(patternID, projectPath string) error {
	return s.withRetry("LinkPatternToProject", func() error {
		_, err := s.db.Exec(`
			INSERT INTO pattern_projects (pattern_id, project_path, uses, last_used)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(pattern_id, project_path) DO UPDATE SET
				uses = pattern_projects.uses + 1,
				last_used = CURRENT_TIMESTAMP
		`, patternID, projectPath)
		return err
	})
}

// GetProjectsForPattern retrieves all projects that use a specific pattern.
// Results are ordered by usage count descending.
func (s *Store) GetProjectsForPattern(patternID string) ([]*PatternProjectLink, error) {
	rows, err := s.db.Query(`
		SELECT pattern_id, project_path, uses, success_count, failure_count, last_used
		FROM pattern_projects
		WHERE pattern_id = ?
		ORDER BY uses DESC
	`, patternID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var links []*PatternProjectLink
	for rows.Next() {
		var link PatternProjectLink
		if err := rows.Scan(&link.PatternID, &link.ProjectPath, &link.Uses, &link.SuccessCount, &link.FailureCount, &link.LastUsed); err != nil {
			return nil, err
		}
		links = append(links, &link)
	}
	return links, rows.Err()
}

// RecordPatternFeedback records feedback when a pattern is applied during an execution.
// Based on the outcome ("success", "failure", or "neutral"), it adjusts the pattern's
// confidence score and updates project-level success/failure counts.
// All three writes (insert feedback, update confidence, update project link) run
// in a single transaction so a partial failure cannot leave the tables inconsistent.
func (s *Store) RecordPatternFeedback(feedback *PatternFeedback) error {
	return s.withRetry("RecordPatternFeedback", func() error {
		tx, err := s.db.BeginTx(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		result, err := tx.Exec(`
			INSERT INTO pattern_feedback (pattern_id, execution_id, project_path, outcome, confidence_delta)
			VALUES (?, ?, ?, ?, ?)
		`, feedback.PatternID, feedback.ExecutionID, feedback.ProjectPath, feedback.Outcome, feedback.ConfidenceDelta)
		if err != nil {
			return err
		}
		id, _ := result.LastInsertId()
		feedback.ID = id

		switch feedback.Outcome {
		case "success":
			if _, err := tx.Exec(`
				UPDATE cross_patterns SET confidence = min(0.95, max(0.1, confidence + ?)) WHERE id = ?
			`, feedback.ConfidenceDelta, feedback.PatternID); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE pattern_projects SET success_count = success_count + 1 WHERE pattern_id = ? AND project_path = ?
			`, feedback.PatternID, feedback.ProjectPath); err != nil {
				return err
			}
		case "failure":
			if _, err := tx.Exec(`
				UPDATE cross_patterns SET confidence = max(0.1, min(0.95, confidence - ?)) WHERE id = ?
			`, feedback.ConfidenceDelta, feedback.PatternID); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE pattern_projects SET failure_count = failure_count + 1 WHERE pattern_id = ? AND project_path = ?
			`, feedback.PatternID, feedback.ProjectPath); err != nil {
				return err
			}
		}

		return tx.Commit()
	})
}

// SearchCrossPatterns searches patterns by title, description, or context using substring matching.
// Results are ordered by confidence and occurrence count descending, up to the specified limit.
func (s *Store) SearchCrossPatterns(query string, limit int) ([]*CrossPattern, error) {
	searchTerm := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT id, pattern_type, title, description, context, examples, confidence, occurrences, is_anti_pattern, scope, created_at, updated_at
		FROM cross_patterns
		WHERE title LIKE ? OR description LIKE ? OR context LIKE ?
		ORDER BY confidence DESC, occurrences DESC
		LIMIT ?
	`, searchTerm, searchTerm, searchTerm, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return s.scanCrossPatterns(rows)
}

// DeleteCrossPattern deletes a cross-project pattern by ID.
// Related pattern_projects and pattern_feedback records are deleted via foreign key cascade.
func (s *Store) DeleteCrossPattern(id string) error {
	return s.withRetry("DeleteCrossPattern", func() error {
		_, err := s.db.Exec(`DELETE FROM cross_patterns WHERE id = ?`, id)
		return err
	})
}

// GetCrossPatternStats returns aggregate statistics about cross-project patterns
// including counts, average confidence, and breakdown by pattern type.
func (s *Store) GetCrossPatternStats() (*CrossPatternStats, error) {
	var stats CrossPatternStats

	// Get total counts
	row := s.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN is_anti_pattern = 0 THEN 1 ELSE 0 END), 0) as patterns,
			COALESCE(SUM(CASE WHEN is_anti_pattern = 1 THEN 1 ELSE 0 END), 0) as anti_patterns,
			COALESCE(AVG(confidence), 0) as avg_confidence,
			COALESCE(SUM(occurrences), 0) as total_occurrences
		FROM cross_patterns
	`)
	if err := row.Scan(&stats.TotalPatterns, &stats.Patterns, &stats.AntiPatterns, &stats.AvgConfidence, &stats.TotalOccurrences); err != nil {
		return nil, err
	}

	// Get type breakdown
	rows, err := s.db.Query(`
		SELECT pattern_type, COUNT(*) as count
		FROM cross_patterns
		GROUP BY pattern_type
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	stats.ByType = make(map[string]int)
	for rows.Next() {
		var pType string
		var count int
		if err := rows.Scan(&pType, &count); err != nil {
			return nil, err
		}
		stats.ByType[pType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get project count
	row = s.db.QueryRow(`SELECT COUNT(DISTINCT project_path) FROM pattern_projects`)
	_ = row.Scan(&stats.ProjectCount)

	return &stats, nil
}

// CrossPatternStats holds aggregate statistics about cross-project patterns.
type CrossPatternStats struct {
	TotalPatterns    int
	Patterns         int
	AntiPatterns     int
	AvgConfidence    float64
	TotalOccurrences int
	ByType           map[string]int
	ProjectCount     int
}
