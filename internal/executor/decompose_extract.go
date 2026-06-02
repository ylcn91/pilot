package executor

import (
	"regexp"
	"strings"
)

// extractNumberedSteps finds numbered list items in text.
// Matches: "1. item", "1) item", "Step 1: item"
func extractNumberedSteps(text string) []string {
	// Pattern for numbered lists
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*\d+[\.\)]\s+(.+)$`),
		regexp.MustCompile(`(?mi)^\s*step\s+\d+[:\s]+(.+)$`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		if len(matches) >= 2 {
			parts := make([]string, 0, len(matches))
			for _, m := range matches {
				if len(m) > 1 {
					parts = append(parts, m[1])
				}
			}
			return parts
		}
	}

	return nil
}

// extractBulletPoints finds bullet list items.
// Matches: "- item", "* item", "• item"
func extractBulletPoints(text string) []string {
	pattern := regexp.MustCompile(`(?m)^\s*[-*•]\s+(.+)$`)
	matches := pattern.FindAllStringSubmatch(text, -1)

	if len(matches) < 2 {
		return nil
	}

	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			// Skip checkbox items that are already marked done
			item := m[1]
			if strings.HasPrefix(item, "[x]") || strings.HasPrefix(item, "[X]") {
				continue
			}
			// Clean checkbox prefix if present
			item = strings.TrimPrefix(item, "[ ] ")
			parts = append(parts, item)
		}
	}

	return parts
}

// extractAcceptanceCriteria finds acceptance criteria sections.
// Matches: "[ ] criteria", "- [ ] criteria"
func extractAcceptanceCriteria(text string) []string {
	pattern := regexp.MustCompile(`(?m)^\s*[-*]?\s*\[\s*\]\s+(.+)$`)
	matches := pattern.FindAllStringSubmatch(text, -1)

	if len(matches) < 2 {
		return nil
	}

	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			parts = append(parts, m[1])
		}
	}

	return parts
}

// extractFileGroups finds file or module groupings.
// Looks for patterns like "file.go", "package/module", "src/component"
func extractFileGroups(text string) []string {
	// Pattern for file paths
	filePattern := regexp.MustCompile(`\b([\w\-]+(?:/[\w\-]+)*\.(?:go|py|ts|tsx|js|jsx|rs|java|rb))\b`)
	matches := filePattern.FindAllString(text, -1)

	if len(matches) < 2 {
		return nil
	}

	// Deduplicate and group by directory
	seen := make(map[string]bool)
	groups := make([]string, 0)
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			groups = append(groups, "Implement changes in "+m)
		}
	}

	return groups
}
