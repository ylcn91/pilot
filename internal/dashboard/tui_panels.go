package dashboard

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// panelStyle bundles the border and title styles used to render a panel,
// letting a single renderer produce both the slate and orange variants.
type panelStyle struct {
	border lipgloss.Style
	label  lipgloss.Style
}

var (
	slatePanelStyle  = panelStyle{border: borderStyle, label: labelStyle}
	orangePanelStyle = panelStyle{border: orangeBorderStyle, label: orangeLabelStyle}
)

// renderPanel builds a slate-bordered panel with guaranteed width.
// tw specifies the total visual width including borders.
// Structure: ╭─ TITLE ─...─╮ / │ (space) content (space) │ / ╰─...─╯
func renderPanel(title string, content string, tw int) string {
	return renderStyledPanel(title, content, tw, slatePanelStyle)
}

// renderOrangePanel renders a panel with orange borders and title (for update notifications).
func renderOrangePanel(title string, content string, tw int) string {
	return renderStyledPanel(title, content, tw, orangePanelStyle)
}

// renderStyledPanel builds a panel manually with guaranteed width using the
// supplied style. Both color variants produce identical layout, differing only
// in the border/label styling.
func renderStyledPanel(title string, content string, tw int, sty panelStyle) string {
	var lines []string

	// Top border: ╭─ TITLE ─────────────────────────────────────────────────────╮
	lines = append(lines, buildTopBorder(title, tw, sty))

	// Empty line padding
	lines = append(lines, buildEmptyLine(tw, sty))

	// Content lines
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, buildContentLine(line, tw, sty))
	}

	// Empty line padding
	lines = append(lines, buildEmptyLine(tw, sty))

	// Bottom border
	lines = append(lines, buildBottomBorder(tw, sty))

	return strings.Join(lines, "\n")
}

// buildTopBorder creates: ╭─ TITLE ─────...─────╮ with exact tw width
func buildTopBorder(title string, tw int, sty panelStyle) string {
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
	return sty.border.Render(prefix) + sty.label.Render(titleUpper) + sty.border.Render(" "+strings.Repeat("─", dashCount)+"╮")
}

// buildBottomBorder creates: ╰─────────────────────────────────────────────────╯
func buildBottomBorder(tw int, sty panelStyle) string {
	// ╰ + dashes + ╯
	dashCount := tw - 2
	line := "╰" + strings.Repeat("─", dashCount) + "╯"
	return sty.border.Render(line)
}

// buildEmptyLine creates: │                                                                 │
func buildEmptyLine(tw int, sty panelStyle) string {
	// │ + spaces + │
	spaceCount := tw - 2
	border := sty.border.Render("│")
	return border + strings.Repeat(" ", spaceCount) + border
}

// buildContentLine creates: │ (space) content padded/truncated (space) │
func buildContentLine(content string, tw int, sty panelStyle) string {
	// Available width for content = tw - 4 (│ + space + space + │)
	contentWidth := tw - 4

	// Pad or truncate content to exact width
	adjusted := padOrTruncate(content, contentWidth)

	// Only style borders, not content
	border := sty.border.Render("│")
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

	// We need to truncate to targetWidth-3 and add "...".
	result := ""
	width := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			result += string(r)
			if r >= 0x40 && r <= 0x7e {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			result += string(r)
			continue
		}
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
