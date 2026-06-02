package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// colorizeGraphChars applies branch track colors to graph characters.
// Track position is determined by the column index of the first branch character.
func colorizeGraphChars(graphStr string) string {
	runes := []rune(graphStr)
	var b strings.Builder

	// Determine colors by tracking position (each 2-char cell = one track column).
	for i, r := range runes {
		track := i / 2 // rough track assignment by character position
		color := branchColors[track%len(branchColors)]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

		switch r {
		case '●', '│', '├', '╌', '╮', '╯':
			b.WriteString(style.Render(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// colorizeRefs applies colors to refs string:
//   - HEAD → main: steel bold
//   - branch names: sage green
//   - tag names (refs/tags/): amber bold
func colorizeRefs(refs string) string {
	if refs == "" {
		return ""
	}

	// Parse individual ref tokens separated by ", "
	tokens := strings.Split(refs, ", ")
	var styled []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		// Strip the long-form prefixes from --decorate=full
		tok = strings.TrimPrefix(tok, "refs/remotes/")
		tok = strings.TrimPrefix(tok, "refs/heads/")
		tok = strings.TrimPrefix(tok, "refs/")

		switch {
		case strings.HasPrefix(tok, "HEAD"):
			styled = append(styled, graphHEADStyle.Render(tok))
		case strings.HasPrefix(tok, "tag: "):
			tagName := strings.TrimPrefix(tok, "tag: ")
			tagName = strings.TrimPrefix(tagName, "tags/")
			styled = append(styled, graphTagStyle.Render(tagName))
		default:
			styled = append(styled, graphBranchStyle.Render(tok))
		}
	}

	if len(styled) == 0 {
		return ""
	}
	return "(" + strings.Join(styled, ", ") + ")"
}

// renderGraphLineFull renders one line in Full mode:
//
//	graph + refs + message + author + SHA (fills width)
func renderGraphLineFull(line GitGraphLine, width int) string {
	graphColored := colorizeGraphChars(line.GraphChars)
	graphWidth := lipgloss.Width(line.GraphChars) // visual width without ANSI

	// If no commit data (pure graph connector), just return the graph part padded.
	if line.SHA == "" {
		padding := width - graphWidth
		if padding < 0 {
			padding = 0
		}
		return graphColored + strings.Repeat(" ", padding)
	}

	// Right-side fixed fields: SHA (7) + space (1) + author (10) = 18 chars
	const shaWidth = 7
	const authorWidth = 10
	const rightFixed = shaWidth + 1 + authorWidth // 18

	styledSHA := graphSHAStyle.Render(fmt.Sprintf("%-7s", line.SHA))
	styledAuthor := graphAuthorStyle.Render(fmt.Sprintf("%-10s", AbbreviateAuthor(line.Author)))
	right := styledSHA + " " + styledAuthor // 18 visual chars

	// Refs: rendered with colors; measure unstyled refs width.
	styledRefs := colorizeRefs(line.Refs)
	refsWidth := 0
	if styledRefs != "" {
		// Measure plain text width: "(" + refs + ") "
		plainRefs := "(" + collapseRefs(line.Refs) + ") "
		refsWidth = lipgloss.Width(plainRefs)
		styledRefs += " " // trailing space
	}

	// Message: fills remaining width
	msgWidth := width - graphWidth - refsWidth - rightFixed - 1 // -1 for space before right
	if msgWidth < 5 {
		msgWidth = 5
	}
	styledMsg := graphMsgStyle.Render(padOrTruncate(line.Message, msgWidth))

	return graphColored + styledRefs + styledMsg + " " + right
}

// renderGraphLineSmall renders one line in Small mode:
//
//	graph + truncated message only (no refs/author/SHA)
func renderGraphLineSmall(line GitGraphLine, width int) string {
	graphColored := colorizeGraphChars(line.GraphChars)
	graphWidth := lipgloss.Width(line.GraphChars)

	if line.SHA == "" {
		padding := width - graphWidth
		if padding < 0 {
			padding = 0
		}
		return graphColored + strings.Repeat(" ", padding)
	}

	msgWidth := width - graphWidth
	if msgWidth < 5 {
		msgWidth = 5
	}
	styledMsg := graphMsgStyle.Render(padOrTruncate(line.Message, msgWidth))
	return graphColored + styledMsg
}

// renderGraphLineMedium renders one line in Medium mode:
//
//	graph + refs + message (no author/SHA)
func renderGraphLineMedium(line GitGraphLine, width int) string {
	graphColored := colorizeGraphChars(line.GraphChars)
	graphWidth := lipgloss.Width(line.GraphChars)

	if line.SHA == "" {
		padding := width - graphWidth
		if padding < 0 {
			padding = 0
		}
		return graphColored + strings.Repeat(" ", padding)
	}

	// Refs
	styledRefs := colorizeRefs(line.Refs)
	refsWidth := 0
	if styledRefs != "" {
		plainRefs := "(" + collapseRefs(line.Refs) + ") "
		refsWidth = lipgloss.Width(plainRefs)
		styledRefs += " "
	}

	msgWidth := width - graphWidth - refsWidth
	if msgWidth < 5 {
		msgWidth = 5
	}
	styledMsg := graphMsgStyle.Render(padOrTruncate(line.Message, msgWidth))
	return graphColored + styledRefs + styledMsg
}

// collapseRefs returns a plain-text version of the refs string for width measurement.
// Strips long-form prefixes (refs/heads/, refs/remotes/, etc.).
func collapseRefs(refs string) string {
	if refs == "" {
		return ""
	}
	tokens := strings.Split(refs, ", ")
	var out []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		tok = strings.TrimPrefix(tok, "refs/remotes/")
		tok = strings.TrimPrefix(tok, "refs/heads/")
		tok = strings.TrimPrefix(tok, "refs/")
		tok = strings.TrimPrefix(tok, "tag: tags/")
		tok = strings.TrimPrefix(tok, "tag: ")
		if tok != "" {
			out = append(out, tok)
		}
	}
	return strings.Join(out, ", ")
}
