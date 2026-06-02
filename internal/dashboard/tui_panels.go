package dashboard

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderPanel builds a panel manually with guaranteed width.
// tw specifies the total visual width including borders.
// Structure: ╭─ TITLE ─...─╮ / │ (space) content (space) │ / ╰─...─╯
func renderPanel(title string, content string, tw int) string {
	var lines []string

	// Top border: ╭─ TITLE ─────────────────────────────────────────────────────╮
	lines = append(lines, buildTopBorder(title, tw))

	// Empty line padding
	lines = append(lines, buildEmptyLine(tw))

	// Content lines
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, buildContentLine(line, tw))
	}

	// Empty line padding
	lines = append(lines, buildEmptyLine(tw))

	// Bottom border
	lines = append(lines, buildBottomBorder(tw))

	return strings.Join(lines, "\n")
}

// buildTopBorder creates: ╭─ TITLE ─────...─────╮ with exact tw width
func buildTopBorder(title string, tw int) string {
	// Characters: ╭ (1) + ─ (1) + space (1) + TITLE + space (1) + dashes + ╮ (1)
	titleUpper := strings.ToUpper(title)
	prefix := "╭─ "
	prefixWidth := lipgloss.Width(prefix + titleUpper + " ")

	// Calculate dashes needed (each ─ is 1 visual char)
	dashCount := tw - prefixWidth - 1 // -1 for ╮
	if dashCount < 0 {
		dashCount = 0
	}

	// Style border chars dim, title bright
	return borderStyle.Render(prefix) + labelStyle.Render(titleUpper) + borderStyle.Render(" "+strings.Repeat("─", dashCount)+"╮")
}

// buildBottomBorder creates: ╰─────────────────────────────────────────────────╯
func buildBottomBorder(tw int) string {
	// ╰ + dashes + ╯
	dashCount := tw - 2
	line := "╰" + strings.Repeat("─", dashCount) + "╯"
	return borderStyle.Render(line)
}

// buildEmptyLine creates: │                                                                 │
func buildEmptyLine(tw int) string {
	// │ + spaces + │
	spaceCount := tw - 2
	border := borderStyle.Render("│")
	return border + strings.Repeat(" ", spaceCount) + border
}

// buildContentLine creates: │ (space) content padded/truncated (space) │
func buildContentLine(content string, tw int) string {
	// Available width for content = tw - 4 (│ + space + space + │)
	contentWidth := tw - 4

	// Pad or truncate content to exact width
	adjusted := padOrTruncate(content, contentWidth)

	// Only style borders, not content
	border := borderStyle.Render("│")
	return border + " " + adjusted + " " + border
}

// renderOrangePanel renders a panel with orange borders and title (for update notifications)
func renderOrangePanel(title string, content string, tw int) string {
	var lines []string

	// Top border
	lines = append(lines, buildOrangeTopBorder(title, tw))

	// Empty line padding
	lines = append(lines, buildOrangeEmptyLine(tw))

	// Content lines
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, buildOrangeContentLine(line, tw))
	}

	// Empty line padding
	lines = append(lines, buildOrangeEmptyLine(tw))

	// Bottom border
	lines = append(lines, buildOrangeBottomBorder(tw))

	return strings.Join(lines, "\n")
}

// buildOrangeTopBorder creates orange top border: ╭─ TITLE ─────...─────╮
func buildOrangeTopBorder(title string, tw int) string {
	titleUpper := strings.ToUpper(title)
	prefix := "╭─ "
	prefixWidth := lipgloss.Width(prefix + titleUpper + " ")

	dashCount := tw - prefixWidth - 1
	if dashCount < 0 {
		dashCount = 0
	}

	return orangeBorderStyle.Render(prefix) + orangeLabelStyle.Render(titleUpper) + orangeBorderStyle.Render(" "+strings.Repeat("─", dashCount)+"╮")
}

// buildOrangeBottomBorder creates orange bottom border: ╰─────────────────────────────────────────────────╯
func buildOrangeBottomBorder(tw int) string {
	dashCount := tw - 2
	line := "╰" + strings.Repeat("─", dashCount) + "╯"
	return orangeBorderStyle.Render(line)
}

// buildOrangeEmptyLine creates orange bordered empty line: │                                                                 │
func buildOrangeEmptyLine(tw int) string {
	spaceCount := tw - 2
	border := orangeBorderStyle.Render("│")
	return border + strings.Repeat(" ", spaceCount) + border
}

// buildOrangeContentLine creates orange bordered content line: │ (space) content padded/truncated (space) │
func buildOrangeContentLine(content string, tw int) string {
	contentWidth := tw - 4
	adjusted := padOrTruncate(content, contentWidth)
	border := orangeBorderStyle.Render("│")
	return border + " " + adjusted + " " + border
}

// padOrTruncate ensures content is exactly targetWidth visual chars
func padOrTruncate(s string, targetWidth int) string {
	visualWidth := lipgloss.Width(s)

	if visualWidth == targetWidth {
		return s
	}

	if visualWidth > targetWidth {
		return truncateVisual(s, targetWidth)
	}

	// Pad with spaces
	return s + strings.Repeat(" ", targetWidth-visualWidth)
}

// truncateVisual truncates string to targetWidth visual chars, adding "..." only if needed
func truncateVisual(s string, targetWidth int) string {
	visualWidth := lipgloss.Width(s)

	// If string already fits, return as-is (no truncation needed)
	if visualWidth <= targetWidth {
		return s
	}

	if targetWidth <= 3 {
		return strings.Repeat(".", targetWidth)
	}

	// We need to truncate to targetWidth-3 and add "..."
	result := ""
	width := 0
	for _, r := range s {
		runeWidth := lipgloss.Width(string(r))
		if width+runeWidth > targetWidth-3 {
			break
		}
		result += string(r)
		width += runeWidth
	}

	// Pad to exactly targetWidth-3 if needed (in case of wide chars)
	for width < targetWidth-3 {
		result += " "
		width++
	}

	return result + "..."
}
