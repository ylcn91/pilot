package memory

import (
	"context"
	"fmt"
	"time"
)

// PatternExtractor extracts patterns from execution results
type PatternExtractor struct {
	store          *GlobalPatternStore
	execStore      *Store
	minOccurrences int
	minConfidence  float64
}

// NewPatternExtractor creates a new pattern extractor
func NewPatternExtractor(patternStore *GlobalPatternStore, execStore *Store) *PatternExtractor {
	return &PatternExtractor{
		store:          patternStore,
		execStore:      execStore,
		minOccurrences: 3,
		minConfidence:  0.7,
	}
}

// ExtractedPattern represents a pattern found in execution output
type ExtractedPattern struct {
	Type        PatternType
	Title       string
	Description string
	Examples    []string
	Confidence  float64
	Context     string // e.g., "Go handlers", "React components"
}

// ExtractionResult holds the result of pattern extraction
type ExtractionResult struct {
	ExecutionID  string
	ProjectPath  string
	Patterns     []*ExtractedPattern
	AntiPatterns []*ExtractedPattern
	ExtractedAt  time.Time
	// Tier is the severity tier parsed from a STANDARD_VIOLATION self-review
	// marker (blocker/must/nice). Empty when no standard violation was found.
	Tier string
}

// ExtractFromExecution extracts patterns from a completed execution
func (e *PatternExtractor) ExtractFromExecution(ctx context.Context, exec *Execution) (*ExtractionResult, error) {
	if exec.Status != "completed" {
		return nil, fmt.Errorf("can only extract patterns from completed executions")
	}

	result := &ExtractionResult{
		ExecutionID:  exec.ID,
		ProjectPath:  exec.ProjectPath,
		Patterns:     make([]*ExtractedPattern, 0),
		AntiPatterns: make([]*ExtractedPattern, 0),
		ExtractedAt:  time.Now(),
	}

	// Extract code patterns from output
	codePatterns := e.extractCodePatterns(exec.Output)
	result.Patterns = append(result.Patterns, codePatterns...)

	// Extract error patterns (anti-patterns)
	if exec.Error != "" {
		errorPatterns := e.extractErrorPatterns(exec.Error)
		result.AntiPatterns = append(result.AntiPatterns, errorPatterns...)
	}

	// Extract workflow patterns
	workflowPatterns := e.extractWorkflowPatterns(exec.Output)
	result.Patterns = append(result.Patterns, workflowPatterns...)

	return result, nil
}

// ExtractAndSave is a convenience method that extracts and saves patterns
func (e *PatternExtractor) ExtractAndSave(ctx context.Context, exec *Execution) error {
	result, err := e.ExtractFromExecution(ctx, exec)
	if err != nil {
		return err
	}

	if len(result.Patterns) == 0 && len(result.AntiPatterns) == 0 {
		return nil // Nothing to save
	}

	return e.SaveExtractedPatterns(ctx, result)
}
