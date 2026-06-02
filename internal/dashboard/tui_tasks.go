package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// taskStatePriority returns sort priority for task states (lower = higher in list).
func taskStatePriority(status string) int {
	switch status {
	case "done":
		return 0
	case "running":
		return 1
	case "queued":
		return 2
	case "pending":
		return 3
	case "failed":
		return 4
	default:
		return 5
	}
}

// renderTasks renders the tasks list with state-aware sorting and rendering.
func (m Model) renderTasks() string {
	var content strings.Builder

	if len(m.tasks) == 0 {
		content.WriteString("  No tasks in queue")
	} else {
		// Sort by state priority, then by ID within same state
		sorted := make([]TaskDisplay, len(m.tasks))
		copy(sorted, m.tasks)
		sort.SliceStable(sorted, func(i, j int) bool {
			pi, pj := taskStatePriority(sorted[i].Status), taskStatePriority(sorted[j].Status)
			if pi != pj {
				return pi < pj
			}
			return sorted[i].ID < sorted[j].ID
		})

		queueIdx := 0 // shimmer offset counter for queued items
		for i, task := range sorted {
			if i > 0 {
				content.WriteString("\n")
			}
			offset := 0
			if task.Status == "queued" {
				offset = queueIdx
				queueIdx++
			}
			content.WriteString(m.renderTask(task, i == m.selectedTask, offset))
		}
	}

	return renderPanel("QUEUE", content.String(), m.effectivePanelTotalWidth())
}

// renderTask renders a single task row with state-aware icons, bars, and meta.
//
// Layout (65 inner chars):
//
//	sel(2) + icon+state(8) + space(1) + id(7) + space(1) + title(20) + gap(2) + bar(16) + gap(1) + meta(5)
func (m Model) renderTask(task TaskDisplay, selected bool, queueOffset int) string {
	var icon, stateLabel, meta string
	var iconStyle, barStyle lipgloss.Style

	switch task.Status {
	case "done":
		icon = "✓"
		stateLabel = "done"
		meta = extractPRNumber(task.PRURL)
		iconStyle = statusDoneStyle
		barStyle = progressBarDoneStyle
	case "running":
		icon = "●"
		stateLabel = "running"
		meta = fmt.Sprintf("%4d%%", task.Progress)
		iconStyle = statusRunningStyle
		barStyle = progressBarStyle
	case "queued":
		icon = "◌"
		stateLabel = "queued"
		meta = fmt.Sprintf("  #%d", queueOffset+1)
		iconStyle = statusQueuedStyle
	case "failed":
		icon = "✗"
		stateLabel = "failed"
		meta = truncateVisual(task.Phase, 5)
		iconStyle = statusFailedStyle
		barStyle = progressBarFailedStyle
	default: // pending
		icon = "·"
		stateLabel = "pending"
		meta = ""
		iconStyle = statusPendingStyle
	}

	// Pulse the running icon on animation tick
	renderedIcon := iconStyle.Render(icon)
	if task.Status == "running" && !m.sparklineTick {
		renderedIcon = dimStyle.Render(icon)
	}

	// Build icon+state column (8 chars visual: "● running" or "✓ done   ")
	iconState := renderedIcon + " " + iconStyle.Render(fmt.Sprintf("%-7s", stateLabel))

	// Selector
	selector := "  "
	if selected {
		selector = dimStyle.Render("▸") + " "
	}

	// Progress bar (14 chars inside brackets, 16 total with [])
	var progressBar string
	switch task.Status {
	case "done":
		bar := barStyle.Render(strings.Repeat("█", 14))
		progressBar = "[" + bar + "]"
	case "running":
		progressBar = m.renderProgressBar(task.Progress, 14)
	case "queued":
		progressBar = m.renderShimmerBar(14, queueOffset)
	case "failed":
		progressBar = m.renderFailedBar(task.Progress, 14)
	default: // pending
		bar := progressEmptyStyle.Render(strings.Repeat(" ", 14))
		progressBar = "[" + bar + "]"
	}

	// Render meta with state-appropriate color
	renderedMeta := iconStyle.Render(fmt.Sprintf("%5s", meta))

	// Left side: selector + icon+state + id + title
	// Right side: bar + meta (right-aligned)
	return fmt.Sprintf("%s%s %-7s %-20s  %s %s",
		selector,
		iconState,
		task.ID,
		truncateVisual(task.Title, 20),
		progressBar,
		renderedMeta,
	)
}

// renderProgressBar renders a standard progress bar for running tasks.
func (m Model) renderProgressBar(progress int, width int) string {
	filled := progress * width / 100
	empty := width - filled

	bar := progressBarStyle.Render(strings.Repeat("█", filled)) +
		progressEmptyStyle.Render(strings.Repeat("░", empty))

	return "[" + bar + "]"
}

// renderFailedBar renders a progress bar frozen at the failure point in dusty rose.
func (m Model) renderFailedBar(progress int, width int) string {
	filled := progress * width / 100
	empty := width - filled

	bar := progressBarFailedStyle.Render(strings.Repeat("█", filled)) +
		progressEmptyStyle.Render(strings.Repeat("░", empty))

	return "[" + bar + "]"
}

// renderShimmerBar renders an animated shimmer bar for queued tasks.
// A 3-char bright spot (░▒▓▒░) slides across the bar, staggered by offset.
func (m Model) renderShimmerBar(width, offset int) string {
	bar := make([]rune, width)
	for i := range bar {
		bar[i] = '░'
	}

	// Center of bright spot, staggered per queue position
	center := (m.shimmerTick + offset*3) % width

	// Apply shimmer pattern: ░▒▓▒░
	type shimmerChar struct {
		offset int
		char   rune
		style  lipgloss.Style
	}
	pattern := []shimmerChar{
		{-2, '░', shimmerDimStyle},
		{-1, '▒', shimmerMidStyle},
		{0, '▓', shimmerBrightStyle},
		{1, '▒', shimmerMidStyle},
		{2, '░', shimmerDimStyle},
	}

	// Build styled string character by character
	var result strings.Builder
	result.WriteString("[")
	for i := 0; i < width; i++ {
		styled := false
		for _, p := range pattern {
			pos := (center + p.offset + width) % width
			if pos == i {
				result.WriteString(p.style.Render(string(p.char)))
				styled = true
				break
			}
		}
		if !styled {
			result.WriteString(shimmerDimStyle.Render("░"))
		}
	}
	result.WriteString("]")
	return result.String()
}

// extractPRNumber extracts "#1234" from a GitHub PR URL like "https://github.com/owner/repo/pull/1234".
// Returns empty string if URL is empty or doesn't match.
func extractPRNumber(prURL string) string {
	if prURL == "" {
		return ""
	}
	// Find last "/" and extract number after it
	idx := strings.LastIndex(prURL, "/")
	if idx >= 0 && idx < len(prURL)-1 {
		num := prURL[idx+1:]
		return fmt.Sprintf("#%s", num)
	}
	return ""
}
