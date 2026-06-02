package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LearnFromDiff analyzes a code diff and extracts potential patterns
func (l *LearningLoop) LearnFromDiff(ctx context.Context, projectPath, diff string, success bool) error {
	// Create a synthetic execution to extract patterns from
	exec := &Execution{
		ID:          fmt.Sprintf("diff_%d", time.Now().UnixNano()),
		ProjectPath: projectPath,
		Status:      "completed",
		Output:      diff,
	}

	if !success {
		exec.Status = "failed"
	}

	if l.extractor != nil {
		return l.extractor.ExtractAndSave(ctx, exec)
	}

	return nil
}

// LearnFromReview processes PR review comments and extracts patterns.
// Approved reviews boost confidence of patterns used in the execution.
// Changes-requested reviews extract anti-patterns from reviewer feedback.
func (l *LearningLoop) LearnFromReview(ctx context.Context, projectPath string,
	reviews []*ReviewData, prURL string) error {
	if len(reviews) == 0 {
		return nil
	}

	if l.extractor == nil {
		return fmt.Errorf("pattern extractor is required for review learning")
	}

	// Collect all review comments for extraction
	comments := make([]string, 0)
	var approvedComments []string

	for _, review := range reviews {
		// Skip empty body reviews (approval clicks without text)
		if strings.TrimSpace(review.Body) == "" {
			continue
		}

		comments = append(comments, review.Body)

		if review.State == "APPROVED" {
			approvedComments = append(approvedComments, review.Body)
		}
	}

	if len(comments) == 0 {
		return nil // No meaningful reviews
	}

	// Extract patterns from review comments
	result, err := l.extractor.ExtractFromReviewComments(ctx, comments, projectPath)
	if err != nil {
		return err
	}

	// Mark change-requested reviews as anti-patterns
	for _, p := range result.AntiPatterns {
		p.Confidence = min(0.85, p.Confidence)
	}

	// Boost confidence for approved reviews with positive patterns
	if len(approvedComments) > 0 {
		for _, p := range result.Patterns {
			p.Confidence = min(0.95, p.Confidence+0.15)
		}
	}

	// Save extracted patterns
	if len(result.Patterns) > 0 || len(result.AntiPatterns) > 0 {
		return l.extractor.SaveExtractedPatterns(ctx, result)
	}

	return nil
}

// LearnFromCIFailure extracts patterns from CI failure logs and saves them.
// It builds a synthetic extraction result from the CI logs, tags patterns with
// CI check names, and persists them via the extractor's SaveExtractedPatterns.
func (l *LearningLoop) LearnFromCIFailure(ctx context.Context, projectPath string, ciLogs string, checkNames []string) error {
	if l.extractor == nil {
		return fmt.Errorf("pattern extractor is required for CI failure learning")
	}

	if strings.TrimSpace(ciLogs) == "" {
		return nil
	}

	// Extract CI-specific patterns (confidence 0.5, source:ci tagged, categorized)
	ciPatterns := l.extractor.extractCIErrorPatterns(ciLogs, checkNames...)
	if len(ciPatterns) == 0 {
		return nil
	}

	result := &ExtractionResult{
		ExecutionID:  fmt.Sprintf("ci_failure_%d", time.Now().UnixNano()),
		ProjectPath:  projectPath,
		Patterns:     make([]*ExtractedPattern, 0),
		AntiPatterns: ciPatterns,
		ExtractedAt:  time.Now(),
	}

	return l.extractor.SaveExtractedPatterns(ctx, result)
}
