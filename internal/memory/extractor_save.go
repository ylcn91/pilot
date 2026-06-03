package memory

import (
	"context"
	"fmt"
	"strings"
)

// SaveExtractedPatterns saves extracted patterns to the store
func (e *PatternExtractor) SaveExtractedPatterns(ctx context.Context, result *ExtractionResult) error {
	for _, p := range result.Patterns {
		globalPattern := &GlobalPattern{
			Type:        p.Type,
			Title:       p.Title,
			Description: p.Description,
			Examples:    p.Examples,
			Confidence:  p.Confidence,
			Projects:    []string{result.ProjectPath},
			Metadata: map[string]interface{}{
				"context":         p.Context,
				"execution_id":    result.ExecutionID,
				"extracted_at":    result.ExtractedAt,
				"is_anti_pattern": false,
			},
		}

		// Check if similar pattern exists
		if existing := e.findSimilarPattern(globalPattern); existing != nil {
			// Merge with existing pattern
			e.mergePattern(existing, globalPattern, result.ProjectPath)
			if err := e.store.Add(existing); err != nil {
				return fmt.Errorf("failed to update pattern: %w", err)
			}
		} else {
			if err := e.store.Add(globalPattern); err != nil {
				return fmt.Errorf("failed to save pattern: %w", err)
			}
		}
	}

	// Save anti-patterns with special marker. When the extraction carried a
	// STANDARD_VIOLATION severity tier, route it into the saved confidence
	// (blocker/must outrank nice) and persist the tier so it stays queryable.
	tierConfidence := tierToConfidence(result.Tier)
	for _, p := range result.AntiPatterns {
		confidence := p.Confidence
		if tierConfidence > confidence {
			confidence = tierConfidence
		}
		metadata := map[string]interface{}{
			"context":         p.Context,
			"execution_id":    result.ExecutionID,
			"extracted_at":    result.ExtractedAt,
			"is_anti_pattern": true,
		}
		if result.Tier != "" {
			metadata["tier"] = result.Tier
		}
		globalPattern := &GlobalPattern{
			Type:        p.Type,
			Title:       "[ANTI] " + p.Title,
			Description: "AVOID: " + p.Description,
			Examples:    p.Examples,
			Confidence:  confidence,
			Projects:    []string{result.ProjectPath},
			Metadata:    metadata,
		}

		// Recurrence detection: if a similar anti-pattern already exists,
		// boost confidence instead of creating a duplicate.
		effectiveConfidence := confidence
		if existing := e.findSimilarPattern(globalPattern); existing != nil {
			isCISourced := strings.HasPrefix(fmt.Sprintf("%v", existing.Metadata["context"]), "source:ci") ||
				strings.HasPrefix(p.Context, "source:ci")
			if isCISourced {
				// CI recurrence: boost by 1.5x, capped at 0.95
				existing.Confidence = min(0.95, existing.Confidence*1.5)
			} else {
				e.mergePattern(existing, globalPattern, result.ProjectPath)
			}
			effectiveConfidence = existing.Confidence
			if err := e.store.Add(existing); err != nil {
				return fmt.Errorf("failed to update anti-pattern: %w", err)
			}
		} else {
			if err := e.store.Add(globalPattern); err != nil {
				return fmt.Errorf("failed to save anti-pattern: %w", err)
			}
		}

		// Also persist CI-sourced anti-patterns to SQLite CrossPattern store
		// so they are visible to PatternContext.InjectPatterns().
		if strings.HasPrefix(p.Context, "source:ci") && e.execStore != nil {
			crossPattern := &CrossPattern{
				ID:            fmt.Sprintf("ci_%s_%s", p.Type, strings.ReplaceAll(strings.ToLower(p.Title), " ", "_")),
				Type:          string(p.Type),
				Title:         "[ANTI] " + p.Title,
				Description:   "AVOID: " + p.Description,
				Context:       p.Context,
				Examples:      p.Examples,
				Confidence:    effectiveConfidence,
				Occurrences:   1,
				IsAntiPattern: true,
				Scope:         "project",
			}
			_ = e.execStore.SaveCrossPattern(crossPattern)
			_ = e.execStore.LinkPatternToProject(crossPattern.ID, result.ProjectPath)
		}
	}

	return nil
}

// findSimilarPattern finds an existing similar pattern
func (e *PatternExtractor) findSimilarPattern(pattern *GlobalPattern) *GlobalPattern {
	existing := e.store.GetByType(pattern.Type)
	// Index by lowercased title for O(1) lookup. Keeping the first occurrence per
	// key preserves the prior "first EqualFold match wins" semantics.
	byTitle := make(map[string]*GlobalPattern, len(existing))
	for _, p := range existing {
		key := strings.ToLower(p.Title)
		if _, ok := byTitle[key]; !ok {
			byTitle[key] = p
		}
	}
	return byTitle[strings.ToLower(pattern.Title)]
}

// mergePattern merges a new pattern into an existing one
func (e *PatternExtractor) mergePattern(existing, new *GlobalPattern, projectPath string) {
	// Add project if not already present
	projectExists := false
	for _, p := range existing.Projects {
		if p == projectPath {
			projectExists = true
			break
		}
	}
	if !projectExists {
		existing.Projects = append(existing.Projects, projectPath)
	}

	// Merge examples (deduplicate)
	exampleSet := make(map[string]bool)
	for _, ex := range existing.Examples {
		exampleSet[ex] = true
	}
	for _, ex := range new.Examples {
		if !exampleSet[ex] {
			existing.Examples = append(existing.Examples, ex)
			exampleSet[ex] = true
		}
	}

	// Increase confidence based on multiple occurrences
	// More projects seeing same pattern = higher confidence
	projectCount := float64(len(existing.Projects))
	existing.Confidence = min(0.95, 0.5+(projectCount*0.1))
}
