package executor

import "strings"

// truncateText truncates text to maxLen and adds ellipsis
func truncateText(text string, maxLen int) string {
	// Remove newlines for display
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

// min returns the smaller of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractCommitSHA extracts git commit SHA from tool output
// Pattern: "[branch abc1234]" or "[main abc1234]" from git commit output
func extractCommitSHA(content string, state *progressState) {
	// Look for git commit output pattern: [branch sha]
	// Example: "[main abc1234] feat: add feature"
	// Example: "[pilot/TASK-123 def5678] fix: bug"
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}

		// Find closing bracket
		closeBracket := strings.Index(line, "]")
		if closeBracket == -1 {
			continue
		}

		// Extract branch and SHA: "[branch sha]"
		inside := line[1:closeBracket]
		parts := strings.Fields(inside)
		if len(parts) >= 2 {
			sha := parts[len(parts)-1]
			// Validate SHA format (7-40 hex characters)
			if isValidSHA(sha) {
				state.commitSHAs = append(state.commitSHAs, sha)
			}
		}
	}
}

// isValidSHA checks if a string looks like a git SHA (7-40 hex chars)
func isValidSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}
	return true
}
