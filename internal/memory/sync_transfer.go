package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExportPatterns exports patterns to a file for sharing
func (ps *PatternSync) ExportPatterns(ctx context.Context, outputPath string, minConfidence float64) error {
	patterns := ps.GetTopOrgPatterns(100)

	var exported []*AggregatedPattern
	for _, p := range patterns {
		if p.Confidence >= minConfidence {
			// Anonymize project paths
			for i := range p.Projects {
				p.Projects[i].ProjectPath = filepath.Base(p.Projects[i].ProjectPath)
			}
			exported = append(exported, p)
		}
	}

	data, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal patterns: %w", err)
	}

	return os.WriteFile(outputPath, data, 0644)
}

// ImportPatterns imports patterns from a file
func (ps *PatternSync) ImportPatterns(ctx context.Context, inputPath string, scope PatternScope) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	var patterns []*AggregatedPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		return fmt.Errorf("failed to parse patterns: %w", err)
	}

	for _, p := range patterns {
		// Convert to cross pattern
		crossPattern := &CrossPattern{
			ID:            p.ID,
			Type:          p.Type,
			Title:         p.Title,
			Description:   p.Description,
			Context:       p.Context,
			Examples:      p.Examples,
			Confidence:    p.Confidence * 0.8, // Reduce confidence for imported patterns
			Occurrences:   1,
			IsAntiPattern: p.IsAntiPattern,
			Scope:         string(scope),
		}

		if err := ps.store.SaveCrossPattern(crossPattern); err != nil {
			return fmt.Errorf("failed to save pattern %s: %w", p.ID, err)
		}
	}

	return nil
}

// PromoteToOrg promotes a project pattern to org scope
func (ps *PatternSync) PromoteToOrg(ctx context.Context, patternID string) error {
	pattern, err := ps.store.GetCrossPattern(patternID)
	if err != nil {
		return fmt.Errorf("pattern not found: %w", err)
	}

	pattern.Scope = "org"
	return ps.store.SaveCrossPattern(pattern)
}

// DemoteToProject demotes an org pattern to project scope
func (ps *PatternSync) DemoteToProject(ctx context.Context, patternID, projectPath string) error {
	pattern, err := ps.store.GetCrossPattern(patternID)
	if err != nil {
		return fmt.Errorf("pattern not found: %w", err)
	}

	pattern.Scope = "project"
	if err := ps.store.SaveCrossPattern(pattern); err != nil {
		return err
	}

	// Ensure link to project
	return ps.store.LinkPatternToProject(patternID, projectPath)
}
