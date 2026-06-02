package memory

import (
	"context"
	"fmt"
	"time"
)

// FeedbackOutcome represents the outcome when a pattern is applied
type FeedbackOutcome string

const (
	OutcomeSuccess FeedbackOutcome = "success"
	OutcomeFailure FeedbackOutcome = "failure"
	OutcomeNeutral FeedbackOutcome = "neutral"
)

// ReviewData represents a PR review comment
type ReviewData struct {
	Body     string // Review comment text
	State    string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED"
	Reviewer string // Reviewer login
}

// LearningLoop implements the pattern learning feedback loop
type LearningLoop struct {
	store          *Store
	extractor      *PatternExtractor
	feedbackWeight float64
	decayRate      float64
}

// LearningConfig configures the learning loop
type LearningConfig struct {
	FeedbackWeight float64 // How much each feedback affects confidence (default: 0.1)
	DecayRate      float64 // Monthly decay for unused patterns (default: 0.01)
}

// DefaultLearningConfig returns default learning configuration
func DefaultLearningConfig() *LearningConfig {
	return &LearningConfig{
		FeedbackWeight: 0.1,
		DecayRate:      0.01,
	}
}

// NewLearningLoop creates a new learning loop
func NewLearningLoop(store *Store, extractor *PatternExtractor, config *LearningConfig) *LearningLoop {
	if config == nil {
		config = DefaultLearningConfig()
	}

	return &LearningLoop{
		store:          store,
		extractor:      extractor,
		feedbackWeight: config.FeedbackWeight,
		decayRate:      config.DecayRate,
	}
}

// RecordExecution records an execution and updates patterns accordingly
func (l *LearningLoop) RecordExecution(ctx context.Context, exec *Execution, appliedPatterns []string) error {
	// Determine outcome based on execution status
	var outcome FeedbackOutcome
	switch exec.Status {
	case "completed":
		outcome = OutcomeSuccess
	case "failed":
		outcome = OutcomeFailure
	default:
		outcome = OutcomeNeutral
	}

	// Record feedback for each applied pattern
	for _, patternID := range appliedPatterns {
		feedback := &PatternFeedback{
			PatternID:       patternID,
			ExecutionID:     exec.ID,
			ProjectPath:     exec.ProjectPath,
			Outcome:         string(outcome),
			ConfidenceDelta: l.calculateConfidenceDelta(outcome),
		}

		if err := l.store.RecordPatternFeedback(feedback); err != nil {
			return fmt.Errorf("failed to record feedback for pattern %s: %w", patternID, err)
		}
	}

	// If successful, extract new patterns from the execution
	if outcome == OutcomeSuccess && l.extractor != nil {
		if err := l.extractor.ExtractAndSave(ctx, exec); err != nil {
			// Log but don't fail - pattern extraction is optional
			_ = err
		}
	}

	return nil
}

// calculateConfidenceDelta calculates how much confidence should change
func (l *LearningLoop) calculateConfidenceDelta(outcome FeedbackOutcome) float64 {
	switch outcome {
	case OutcomeSuccess:
		return l.feedbackWeight
	case OutcomeFailure:
		return l.feedbackWeight * 1.5 // Failures have more impact
	default:
		return 0
	}
}

// ApplyDecay applies confidence decay to unused patterns
func (l *LearningLoop) ApplyDecay(ctx context.Context) (int, error) {
	// Get patterns that haven't been used recently
	staleThreshold := time.Now().AddDate(0, -3, 0) // 3 months

	patterns, err := l.store.GetTopCrossPatterns(1000, 0) // Get all patterns
	if err != nil {
		return 0, fmt.Errorf("failed to get patterns: %w", err)
	}

	updated := 0
	for _, p := range patterns {
		if p.UpdatedAt.Before(staleThreshold) {
			// Apply decay
			newConfidence := p.Confidence * (1 - l.decayRate)
			if newConfidence < 0.1 {
				// Pattern has decayed too much, mark for potential cleanup
				newConfidence = 0.1
			}

			p.Confidence = newConfidence
			if err := l.store.SaveCrossPattern(p); err != nil {
				return updated, fmt.Errorf("failed to update pattern %s: %w", p.ID, err)
			}
			updated++
		}
	}

	return updated, nil
}

// DeprecateLowConfidencePatterns marks or removes patterns with very low confidence
func (l *LearningLoop) DeprecateLowConfidencePatterns(ctx context.Context, threshold float64) (int, error) {
	patterns, err := l.store.GetTopCrossPatterns(1000, 0)
	if err != nil {
		return 0, err
	}

	deprecated := 0
	for _, p := range patterns {
		if p.Confidence < threshold && p.Occurrences < 3 {
			// Low confidence and rarely used - deprecate
			if err := l.store.DeleteCrossPattern(p.ID); err != nil {
				return deprecated, err
			}
			deprecated++
		}
	}

	return deprecated, nil
}
