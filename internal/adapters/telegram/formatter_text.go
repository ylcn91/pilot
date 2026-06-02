package telegram

import (
	"fmt"
	"regexp"
	"strings"
)

// Internal signals to strip from output
var internalSignals = []string{
	"EXIT_SIGNAL: true",
	"EXIT_SIGNAL:true",
	"LOOP COMPLETE",
	"TASK MODE COMPLETE",
	"NAVIGATOR_STATUS",
	"━━━━━━━━━━",
	"Phase:",
	"Iteration:",
	"Progress:",
	"Completion Indicators:",
	"Exit Conditions:",
	"State Hash:",
	"Next Action:",
}

// FormatQuestionAck formats acknowledgment for a question
func FormatQuestionAck() string {
	return "🔍 Looking into that..."
}

// FormatQuestionAnswer formats an answer to a question
func FormatQuestionAnswer(answer string) string {
	// Clean any internal signals from the answer
	cleanAnswer := cleanInternalSignals(answer)

	// Convert markdown tables to lists (Telegram doesn't support tables)
	cleanAnswer = convertTablesToLists(cleanAnswer)

	// Truncate if too long for Telegram
	if len(cleanAnswer) > 3500 {
		cleanAnswer = cleanAnswer[:3500] + "\n\n_(truncated)_"
	}

	return cleanAnswer
}

// convertTablesToLists converts markdown tables to bullet lists
// Telegram doesn't support table formatting
func convertTablesToLists(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	var headers []string
	inTable := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect table header row
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			// Check if next line is separator (|---|---|)
			if i+1 < len(lines) {
				nextLine := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(nextLine, "|") && strings.Contains(nextLine, "---") {
					// This is a header row
					headers = parseTableRow(trimmed)
					inTable = true
					continue
				}
			}

			// Check if this is separator row
			if strings.Contains(trimmed, "---") {
				continue
			}

			// This is a data row
			if inTable && len(headers) > 0 {
				cells := parseTableRow(trimmed)
				// Format as "• Col1: Val1 | Col2: Val2" or just "• Val1 - Val2"
				if len(cells) >= 2 {
					if len(headers) >= 2 && headers[0] != "" {
						// Use first column as key, rest as description
						result = append(result, fmt.Sprintf("• %s: %s", cells[0], strings.Join(cells[1:], " | ")))
					} else {
						result = append(result, fmt.Sprintf("• %s", strings.Join(cells, " - ")))
					}
				} else if len(cells) == 1 {
					result = append(result, fmt.Sprintf("• %s", cells[0]))
				}
				continue
			}
		} else {
			// Not a table row
			if inTable {
				inTable = false
				headers = nil
			}
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// parseTableRow extracts cells from a markdown table row
func parseTableRow(row string) []string {
	// Remove leading/trailing pipes and split
	row = strings.Trim(row, "|")
	parts := strings.Split(row, "|")

	var cells []string
	for _, part := range parts {
		cell := strings.TrimSpace(part)
		if cell != "" && !strings.HasPrefix(cell, "---") {
			cells = append(cells, cell)
		}
	}
	return cells
}

// cleanInternalSignals removes internal Navigator signals from output
func cleanInternalSignals(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	var cleanLines []string
	skipBlock := false

	for _, line := range lines {
		// Skip NAVIGATOR_STATUS blocks
		if strings.Contains(line, "NAVIGATOR_STATUS") {
			skipBlock = true
			continue
		}
		if skipBlock {
			// End of block when we see another separator
			if strings.HasPrefix(strings.TrimSpace(line), "━") && len(cleanLines) > 0 {
				skipBlock = false
			}
			continue
		}

		// Skip lines with internal signals
		shouldSkip := false
		for _, signal := range internalSignals {
			if strings.Contains(line, signal) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			continue
		}

		// Skip empty lines at the start
		if len(cleanLines) == 0 && strings.TrimSpace(line) == "" {
			continue
		}

		cleanLines = append(cleanLines, line)
	}

	// Trim trailing empty lines
	for len(cleanLines) > 0 && strings.TrimSpace(cleanLines[len(cleanLines)-1]) == "" {
		cleanLines = cleanLines[:len(cleanLines)-1]
	}

	return strings.Join(cleanLines, "\n")
}

// extractSummary extracts key summary points from output
func extractSummary(output string) string {
	// Look for common summary patterns
	patterns := []struct {
		regex  string
		format string
	}{
		{`(?i)created?\s+["\x60]?([^"\x60\n]+\.\w+)["\x60]?`, "📁 Created: %s"},
		{`(?i)modified?\s+["\x60]?([^"\x60\n]+\.\w+)["\x60]?`, "📝 Modified: %s"},
		{`(?i)added?\s+["\x60]?([^"\x60\n]+\.\w+)["\x60]?`, "➕ Added: %s"},
		{`(?i)deleted?\s+["\x60]?([^"\x60\n]+\.\w+)["\x60]?`, "🗑 Deleted: %s"},
	}

	var summaryItems []string
	seen := make(map[string]bool)

	for _, p := range patterns {
		re := regexp.MustCompile(p.regex)
		matches := re.FindAllStringSubmatch(output, 5) // Max 5 matches per pattern
		for _, match := range matches {
			if len(match) > 1 {
				item := fmt.Sprintf(p.format, match[1])
				if !seen[item] {
					summaryItems = append(summaryItems, item)
					seen[item] = true
				}
			}
		}
	}

	if len(summaryItems) == 0 {
		return ""
	}

	// Limit to 5 items
	if len(summaryItems) > 5 {
		summaryItems = summaryItems[:5]
		summaryItems = append(summaryItems, "_(and more...)_")
	}

	return strings.Join(summaryItems, "\n")
}

// escapeMarkdown escapes Telegram Markdown special characters
func escapeMarkdown(text string) string {
	// Characters that need escaping in Telegram Markdown
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateDescription truncates a string to maxLen
func truncateDescription(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// chunkContent splits content into chunks of maxLen characters.
// Tries to break at newlines for cleaner output.
func chunkContent(content string, maxLen int) []string {
	if len(content) <= maxLen {
		return []string{content}
	}

	var chunks []string
	remaining := content

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find a good break point (prefer newline)
		breakPoint := maxLen
		if idx := strings.LastIndex(remaining[:maxLen], "\n"); idx > maxLen/2 {
			breakPoint = idx + 1
		}

		chunks = append(chunks, strings.TrimSpace(remaining[:breakPoint]))
		remaining = strings.TrimSpace(remaining[breakPoint:])
	}

	return chunks
}
