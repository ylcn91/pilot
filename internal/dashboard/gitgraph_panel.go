package dashboard

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// refreshGitGraphCmd returns a tea.Cmd that fetches git graph data in the background.
func refreshGitGraphCmd(projectPath string) tea.Cmd {
	return func() tea.Msg {
		state := FetchGitGraph(projectPath, 200)
		return gitRefreshMsg{state: state}
	}
}

// gitRefreshTickCmd returns a 15-second tick that triggers a graph refresh.
func gitRefreshTickCmd() tea.Cmd {
	return tea.Tick(15*time.Second, func(_ time.Time) tea.Msg {
		return gitRefreshTickMsg{}
	})
}

// gitRefreshTickMsg fires every 15 seconds when the graph is visible.
type gitRefreshTickMsg struct{}

// gitGraphViewportHeight returns how many content lines fit in the git graph panel.
// Panel structure: top border(1) + empty line(1) + [content] + empty line(1) + bottom border(1) + scroll indicator(1) = 5 overhead.
func (m Model) gitGraphViewportHeight() int {
	if m.height > 5 {
		return m.height - 5
	}
	return 1
}

// renderGitGraph renders the git graph panel for the current model state.
// Returns an empty string if hidden. Optional variadic args:
//   - opts[0] = forceWidth: panel width (stacked layout uses full terminal width)
//   - opts[1] = forceHeight: panel height (stacked layout uses remaining terminal space)
func (m Model) renderGitGraph(opts ...int) string {
	if m.gitGraph.mode == GitGraphHidden {
		return ""
	}

	var graphWidth int
	var forceHeight int
	if len(opts) > 0 && opts[0] > 0 {
		graphWidth = opts[0]
	}
	if len(opts) > 1 && opts[1] > 0 {
		forceHeight = opts[1]
	}
	if graphWidth == 0 {
		// Side-by-side layout: calculate from remaining terminal width
		graphWidth = 60 // default when terminal width unknown
		if m.width > 0 {
			graphWidth = m.width - panelTotalWidth - 2
		}
	}
	if graphWidth < 20 {
		return ""
	}

	// Auto-select size based on available width.
	// Full needs enough room for graph + refs + message + SHA(7) + author(10) = 18 extra.
	// Generous thresholds keep the graph compact until there's real space.
	size := gitGraphSizeFull
	if graphWidth < 65 {
		size = gitGraphSizeMedium
	}
	if graphWidth < 40 {
		size = gitGraphSizeSmall
	}

	// Title based on size, with project name suffix (GH-2167)
	title := "GIT"
	if size == gitGraphSizeFull {
		title = "GIT GRAPH"
	}
	if m.gitGraph.projectName != "" {
		title += " — " + m.gitGraph.projectName
	}

	// Build content lines
	innerWidth := graphWidth - 4 // border(1) + space(1) + space(1) + border(1)

	var contentLines []string
	var scrollIndicator string

	// Error or loading state
	if m.gitGraph.state == nil {
		contentLines = append(contentLines, "  Loading...")
	} else if m.gitGraph.state.Error != "" {
		contentLines = append(contentLines, "  "+truncateVisual(m.gitGraph.state.Error, innerWidth-2))
	} else {
		lines := m.gitGraph.state.Lines
		total := len(lines)

		// Apply scroll offset
		start := m.gitGraph.scroll
		if start >= total {
			start = 0
		}

		// Calculate visible lines from panel height
		panelHeight := m.height
		if forceHeight > 0 {
			panelHeight = forceHeight
		}
		visibleLines := 30 // fallback when height unknown
		if panelHeight > 0 {
			visibleLines = panelHeight - 5 // borders(2) + padding(2) + scroll indicator(1)
			if visibleLines < 1 {
				visibleLines = 1
			}
		}

		// Clamp scroll offset
		maxScroll := total - visibleLines
		if maxScroll < 0 {
			maxScroll = 0
		}
		if start > maxScroll {
			start = maxScroll
		}

		end := start + visibleLines
		if end > total {
			end = total
		}

		for _, line := range lines[start:end] {
			var rendered string
			switch size {
			case gitGraphSizeSmall:
				rendered = renderGraphLineSmall(line, innerWidth)
			case gitGraphSizeMedium:
				rendered = renderGraphLineMedium(line, innerWidth)
			default:
				rendered = renderGraphLineFull(line, innerWidth)
			}
			contentLines = append(contentLines, rendered)
		}

		// Build scroll indicator
		if total > 0 {
			indicator := fmt.Sprintf("[%d-%d of %d]", start+1, end, total)
			scrollIndicator = padOrTruncate(graphScrollStyle.Render(indicator), innerWidth)
		}
	}

	// Full-height stretch: pad content lines to fill panel height
	stretchHeight := m.height
	if forceHeight > 0 {
		stretchHeight = forceHeight
	}
	if stretchHeight > 0 {
		contentArea := stretchHeight - 4
		if contentArea < 1 {
			contentArea = 1
		}
		indicatorReserve := 0
		if scrollIndicator != "" {
			indicatorReserve = 1
		}
		for len(contentLines) < contentArea-indicatorReserve {
			contentLines = append(contentLines, "")
		}
		if scrollIndicator != "" {
			contentLines = append(contentLines, scrollIndicator)
		}
	} else if scrollIndicator != "" {
		contentLines = append(contentLines, "")
		contentLines = append(contentLines, scrollIndicator)
	}

	return m.renderGraphPanel(title, contentLines, graphWidth)
}

// renderGraphPanel builds a bordered panel at the given total width.
// Focused state uses steel blue border; unfocused uses slate. It reuses the
// shared panel primitives from tui_panels.go via a style-parameterized
// panelStyle, keeping the title bright (labelStyle) like the slate panels.
func (m Model) renderGraphPanel(title string, contentLines []string, totalWidth int) string {
	var borderSty lipgloss.Style
	if m.gitGraph.focus {
		borderSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#7eb8da")) // steel blue
	} else {
		borderSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#3d4450")) // slate
	}
	sty := panelStyle{border: borderSty, label: labelStyle}

	var renderedLines []string
	renderedLines = append(renderedLines, buildTopBorder(title, totalWidth, sty))
	renderedLines = append(renderedLines, buildEmptyLine(totalWidth, sty))
	for _, line := range contentLines {
		renderedLines = append(renderedLines, buildContentLine(line, totalWidth, sty))
	}
	renderedLines = append(renderedLines, buildEmptyLine(totalWidth, sty))
	renderedLines = append(renderedLines, buildBottomBorder(totalWidth, sty))

	return strings.Join(renderedLines, "\n")
}
