package memory

import (
	"context"
	"fmt"
	"sort"
)

// GetPatternPerformance returns performance metrics for a pattern
func (l *LearningLoop) GetPatternPerformance(ctx context.Context, patternID string) (*PatternPerformance, error) {
	pattern, err := l.store.GetCrossPattern(patternID)
	if err != nil {
		return nil, err
	}

	links, err := l.store.GetProjectsForPattern(patternID)
	if err != nil {
		return nil, err
	}

	var totalUses, totalSuccess, totalFailure int
	for _, link := range links {
		totalUses += link.Uses
		totalSuccess += link.SuccessCount
		totalFailure += link.FailureCount
	}

	successRate := 0.0
	if totalSuccess+totalFailure > 0 {
		successRate = float64(totalSuccess) / float64(totalSuccess+totalFailure)
	}

	return &PatternPerformance{
		PatternID:     patternID,
		Title:         pattern.Title,
		Type:          pattern.Type,
		Confidence:    pattern.Confidence,
		TotalUses:     totalUses,
		SuccessCount:  totalSuccess,
		FailureCount:  totalFailure,
		SuccessRate:   successRate,
		ProjectCount:  len(links),
		IsAntiPattern: pattern.IsAntiPattern,
	}, nil
}

// PatternPerformance holds performance metrics for a pattern
type PatternPerformance struct {
	PatternID     string
	Title         string
	Type          string
	Confidence    float64
	TotalUses     int
	SuccessCount  int
	FailureCount  int
	SuccessRate   float64
	ProjectCount  int
	IsAntiPattern bool
}

// GetTopPerformingPatterns returns patterns with the best success rates
func (l *LearningLoop) GetTopPerformingPatterns(ctx context.Context, limit int) ([]*PatternPerformance, error) {
	patterns, err := l.store.GetTopCrossPatterns(100, 0.5)
	if err != nil {
		return nil, err
	}

	var performances []*PatternPerformance
	for _, p := range patterns {
		perf, err := l.GetPatternPerformance(ctx, p.ID)
		if err != nil {
			continue
		}
		performances = append(performances, perf)
	}

	// Sort by success rate (descending). SliceStable preserves the original
	// relative order of equal-rate entries, matching the prior selection sort.
	sort.SliceStable(performances, func(i, j int) bool {
		return performances[i].SuccessRate > performances[j].SuccessRate
	})

	if len(performances) > limit {
		performances = performances[:limit]
	}

	return performances, nil
}

// SurfaceHighValuePatterns returns patterns that should be highlighted
func (l *LearningLoop) SurfaceHighValuePatterns(ctx context.Context, projectPath string) ([]*CrossPattern, error) {
	// Get patterns for the project
	patterns, err := l.store.GetCrossPatternsForProject(projectPath, true)
	if err != nil {
		return nil, err
	}

	// Filter to high-value patterns
	var highValue []*CrossPattern
	for _, p := range patterns {
		// High value = high confidence + multiple uses + successful across projects
		if p.Confidence >= 0.75 && p.Occurrences >= 5 {
			highValue = append(highValue, p)
		}
	}

	// Limit to top 5
	if len(highValue) > 5 {
		highValue = highValue[:5]
	}

	return highValue, nil
}

// RecordMergeOutcome reinforces every pattern linked to a project after one of
// the project's PRs merges. A merge is the strongest available success signal —
// the work shipped and passed review/CI — so it credits each pattern's
// (project, taskType) context with a success outcome.
//
// It mirrors SurfaceHighValuePatterns' use of GetCrossPatternsForProject and is
// idempotent only at the call site: callers must guard against recording the
// same merge twice (see autopilot's reinforcement guard).
func (l *LearningLoop) RecordMergeOutcome(projectPath, taskType, model string) error {
	patterns, err := l.store.GetCrossPatternsForProject(projectPath, false)
	if err != nil {
		return fmt.Errorf("failed to get patterns for merge reinforcement: %w", err)
	}

	for _, p := range patterns {
		if err := l.store.RecordPatternOutcome(p.ID, projectPath, taskType, model, true); err != nil {
			return fmt.Errorf("failed to record merge outcome for pattern %s: %w", p.ID, err)
		}
	}

	return nil
}

// BoostPatternConfidence manually boosts a pattern's confidence
func (l *LearningLoop) BoostPatternConfidence(ctx context.Context, patternID string, amount float64) error {
	pattern, err := l.store.GetCrossPattern(patternID)
	if err != nil {
		return err
	}

	pattern.Confidence = min(0.95, pattern.Confidence+amount)
	return l.store.SaveCrossPattern(pattern)
}

// ResetPatternStats resets a pattern's usage statistics
func (l *LearningLoop) ResetPatternStats(ctx context.Context, patternID string) error {
	pattern, err := l.store.GetCrossPattern(patternID)
	if err != nil {
		return err
	}

	pattern.Occurrences = 1
	pattern.Confidence = 0.5 // Reset to neutral
	return l.store.SaveCrossPattern(pattern)
}
