package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// SyncFromProject synchronizes patterns from a project to org level
func (ps *PatternSync) SyncFromProject(ctx context.Context, projectPath string) error {
	// Get all cross-project patterns from the database
	patterns, err := ps.store.GetCrossPatternsForProject(projectPath, false)
	if err != nil {
		return fmt.Errorf("failed to get project patterns: %w", err)
	}

	// Get project links for each pattern
	for _, p := range patterns {
		links, err := ps.store.GetProjectsForPattern(p.ID)
		if err != nil {
			continue
		}

		// Aggregate into org pattern
		if err := ps.aggregatePattern(p, links); err != nil {
			return fmt.Errorf("failed to aggregate pattern %s: %w", p.ID, err)
		}
	}

	return nil
}

// aggregatePattern aggregates a pattern across all its project mentions
func (ps *PatternSync) aggregatePattern(p *CrossPattern, links []*PatternProjectLink) error {
	existing, _ := ps.orgPatterns.Get(p.ID)

	if existing == nil {
		// Create new aggregated pattern
		existing = &AggregatedPattern{
			ID:            p.ID,
			Type:          p.Type,
			Title:         p.Title,
			Description:   p.Description,
			Context:       p.Context,
			Examples:      p.Examples,
			IsAntiPattern: p.IsAntiPattern,
			CreatedAt:     p.CreatedAt,
		}
	}

	// Update with latest data
	existing.Title = p.Title
	existing.Description = p.Description
	existing.Context = p.Context

	// Merge examples (deduplicate)
	exampleSet := make(map[string]bool)
	for _, ex := range existing.Examples {
		exampleSet[ex] = true
	}
	for _, ex := range p.Examples {
		if !exampleSet[ex] && len(existing.Examples) < 10 {
			existing.Examples = append(existing.Examples, ex)
			exampleSet[ex] = true
		}
	}

	// Calculate aggregated metrics
	existing.Occurrences = 0
	existing.Projects = make([]ProjectMention, 0, len(links))
	totalSuccessRate := 0.0

	for _, link := range links {
		existing.Occurrences += link.Uses
		successRate := 0.0
		total := link.SuccessCount + link.FailureCount
		if total > 0 {
			successRate = float64(link.SuccessCount) / float64(total)
		}
		totalSuccessRate += successRate

		existing.Projects = append(existing.Projects, ProjectMention{
			ProjectPath: link.ProjectPath,
			Uses:        link.Uses,
			SuccessRate: successRate,
			LastUsed:    link.LastUsed,
		})
	}

	existing.ProjectCount = len(links)

	// Calculate confidence based on occurrences and success rate
	if existing.ProjectCount > 0 {
		avgSuccessRate := totalSuccessRate / float64(existing.ProjectCount)
		// Confidence grows with more projects and higher success rate
		baseConfidence := 0.5 + (float64(existing.ProjectCount) * 0.05)      // +5% per project
		existing.Confidence = min(0.95, baseConfidence+(avgSuccessRate*0.3)) // +30% max from success rate
	}

	return ps.orgPatterns.Update(existing)
}

// GetOrgPatterns retrieves all org-level patterns
func (ps *PatternSync) GetOrgPatterns() []*AggregatedPattern {
	return ps.orgPatterns.GetAll()
}

// GetTopOrgPatterns retrieves top org patterns by confidence
func (ps *PatternSync) GetTopOrgPatterns(limit int) []*AggregatedPattern {
	all := ps.orgPatterns.GetAll()

	// Sort by confidence
	sort.Slice(all, func(i, j int) bool {
		return all[i].Confidence > all[j].Confidence
	})

	if len(all) > limit {
		return all[:limit]
	}
	return all
}

// GetOrgPatternsForContext retrieves org patterns relevant to a context
func (ps *PatternSync) GetOrgPatternsForContext(ctx context.Context, taskContext string) ([]*AggregatedPattern, error) {
	all := ps.orgPatterns.GetAll()

	// Score patterns by relevance
	type scored struct {
		pattern *AggregatedPattern
		score   float64
	}

	var results []scored
	for _, p := range all {
		score := p.Confidence

		// Boost if pattern type matches context hints
		if matchesContext(p.Type, taskContext) {
			score += 0.1
		}

		// Boost for high occurrence patterns
		if p.Occurrences > 10 {
			score += 0.1
		}

		// Boost for patterns used across many projects
		if p.ProjectCount > 3 {
			score += 0.1
		}

		results = append(results, scored{pattern: p, score: score})
	}

	// Sort by score
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Return top results
	patterns := make([]*AggregatedPattern, 0, min(10, len(results)))
	for i := 0; i < len(results) && i < 10; i++ {
		patterns = append(patterns, results[i].pattern)
	}

	return patterns, nil
}

// matchesContext checks if a pattern type is relevant to a context
func matchesContext(patternType, context string) bool {
	// Simple keyword matching - could be enhanced with embeddings
	typeContextMap := map[string][]string{
		"code":      {"function", "method", "implement", "write", "create"},
		"structure": {"package", "organize", "refactor", "architecture"},
		"workflow":  {"test", "build", "deploy", "commit", "ci"},
		"error":     {"error", "bug", "fix", "debug"},
		"naming":    {"rename", "name", "convention"},
	}

	keywords, ok := typeContextMap[patternType]
	if !ok {
		return false
	}

	for _, kw := range keywords {
		if contains(context, kw) {
			return true
		}
	}

	return false
}

// contains checks if text contains a keyword (case-insensitive)
func contains(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}
