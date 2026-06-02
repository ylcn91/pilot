package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ExtractFromReviewComments extracts patterns from PR review comments
func (e *PatternExtractor) ExtractFromReviewComments(ctx context.Context,
	comments []string, projectPath string) (*ExtractionResult, error) {
	result := &ExtractionResult{
		ExecutionID:  fmt.Sprintf("review_%d", time.Now().UnixNano()),
		ProjectPath:  projectPath,
		Patterns:     make([]*ExtractedPattern, 0),
		AntiPatterns: make([]*ExtractedPattern, 0),
		ExtractedAt:  time.Now(),
	}

	// Combine all comments for analysis
	combinedComments := strings.Join(comments, " ")

	// Review comment matchers for anti-patterns and positive feedback
	reviewMatchers := []struct {
		patterns   []string // Keywords to match
		pType      PatternType
		title      string
		desc       string
		isAnti     bool
		confidence float64
	}{
		{
			patterns:   []string{"add test", "test coverage", "missing test"},
			pType:      PatternTypeWorkflow,
			title:      "Add tests",
			desc:       "Ensure comprehensive test coverage for all functions",
			isAnti:     true,
			confidence: 0.75,
		},
		{
			patterns:   []string{"naming", "unclear variable", "rename", "confusing name"},
			pType:      PatternTypeNaming,
			title:      "Improve naming",
			desc:       "Use clear, descriptive names for variables and functions",
			isAnti:     true,
			confidence: 0.7,
		},
		{
			patterns:   []string{"error handling", "unchecked error", "handle error"},
			pType:      PatternTypeError,
			title:      "Add error handling",
			desc:       "Properly handle and propagate errors",
			isAnti:     true,
			confidence: 0.75,
		},
		{
			patterns:   []string{"documentation", "add comment", "unclear", "explain"},
			pType:      PatternTypeCode,
			title:      "Add documentation",
			desc:       "Add comments and documentation for clarity",
			isAnti:     true,
			confidence: 0.65,
		},
		{
			patterns:   []string{"good pattern", "nice approach", "well done", "excellent", "clean implementation"},
			pType:      PatternTypeCode,
			title:      "Well-implemented pattern",
			desc:       "This implementation follows good practices",
			isAnti:     false,
			confidence: 0.8,
		},
	}

	// Check each matcher against comments
	for _, matcher := range reviewMatchers {
		found := false
		lowerComments := strings.ToLower(combinedComments)

		for _, pattern := range matcher.patterns {
			if strings.Contains(lowerComments, strings.ToLower(pattern)) {
				found = true
				break
			}
		}

		if found {
			p := &ExtractedPattern{
				Type:        matcher.pType,
				Title:       matcher.title,
				Description: matcher.desc,
				Examples:    comments,
				Confidence:  matcher.confidence,
				Context:     "PR review",
			}

			if matcher.isAnti {
				result.AntiPatterns = append(result.AntiPatterns, p)
			} else {
				result.Patterns = append(result.Patterns, p)
			}
		}
	}

	return result, nil
}
