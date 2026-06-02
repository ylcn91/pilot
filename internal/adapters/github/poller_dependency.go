package github

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/ylcn91/pilot/internal/text"
)

// ExtractPRNumber extracts PR number from a GitHub PR URL
// e.g., "https://github.com/owner/repo/pull/123" -> 123
func ExtractPRNumber(prURL string) (int, error) {
	if prURL == "" {
		return 0, fmt.Errorf("empty PR URL")
	}

	// Match pattern: /pull/123 or /pulls/123
	re := regexp.MustCompile(`/pulls?/(\d+)`)
	matches := re.FindStringSubmatch(prURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not extract PR number from URL: %s", prURL)
	}

	var num int
	if _, err := fmt.Sscanf(matches[1], "%d", &num); err != nil {
		return 0, fmt.Errorf("invalid PR number in URL: %s", prURL)
	}

	return num, nil
}

// dependencyRegex matches common dependency patterns in issue bodies:
// - "Depends on: #123"
// - "Depends on #123"
// - "## Depends on: #123"
// - "Blocked by: #123"
// - "Blocked by #123"
// - "Requires: #123"
// - "Requires #123"
var dependencyRegex = regexp.MustCompile(`(?i)(?:depends\s+on|blocked\s+by|requires):?\s*#(\d+)`)

// ParseDependencies extracts issue numbers that this issue depends on from the body.
// It looks for patterns like "Depends on: #123", "Blocked by: #456", etc.
func ParseDependencies(body string) []int {
	if body == "" {
		return nil
	}

	matches := dependencyRegex.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	// Use a map to deduplicate
	seen := make(map[int]bool)
	var deps []int

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(match[1], "%d", &num); err != nil {
			continue
		}
		if num > 0 && !seen[num] {
			seen[num] = true
			deps = append(deps, num)
		}
	}

	return deps
}

// hasPendingDependencies checks if any of the issue's dependencies are still open.
// Returns true if the issue has open dependencies and should be skipped.
func (p *Poller) hasPendingDependencies(ctx context.Context, issue *Issue) bool {
	// Sanitize before parsing so an attacker cannot smuggle fake dependency
	// references (e.g. an invisible "#1337" that would block execution).
	deps := ParseDependencies(text.SanitizeUntrustedString(issue.Body))
	if len(deps) == 0 {
		return false
	}

	for _, depNum := range deps {
		depIssue, err := p.client.GetIssue(ctx, p.owner, p.repo, depNum)
		if err != nil {
			// If we can't fetch the dependency, log and assume it's still pending
			// to be safe (don't execute if we can't verify)
			p.logger.Warn("Failed to fetch dependency issue",
				slog.Int("issue", issue.Number),
				slog.Int("dependency", depNum),
				slog.Any("error", err),
			)
			return true
		}

		// Check if dependency is still open
		if depIssue.State == "open" {
			p.logger.Debug("Issue has open dependency, skipping",
				slog.Int("issue", issue.Number),
				slog.Int("dependency", depNum),
			)
			return true
		}
	}

	return false
}
