package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// historyGroup represents a top-level entry in the HISTORY panel.
// It is either a standalone task, an active epic (expanded with sub-issues),
// or a completed epic (collapsed to one line).
type historyGroup struct {
	Task      CompletedTask   // The top-level task (standalone or epic parent)
	SubIssues []CompletedTask // Sub-issues (only populated for epics)
}

// groupedHistory transforms the flat completedTasks slice into groups.
// Sub-issues (ParentID != "") are absorbed under their parent epic.
// Standalone tasks and epics without children in the list pass through as-is.
func (m Model) groupedHistory() []historyGroup {
	// Build lookup: ParentID → children
	childrenOf := make(map[string][]CompletedTask)
	parentIDs := make(map[string]bool)
	for _, t := range m.completedTasks {
		if t.ParentID != "" {
			childrenOf[t.ParentID] = append(childrenOf[t.ParentID], t)
		}
		if t.IsEpic {
			parentIDs[t.ID] = true
		}
	}

	var groups []historyGroup
	seen := make(map[string]bool)

	for _, t := range m.completedTasks {
		if seen[t.ID] {
			continue
		}
		// Skip sub-issues whose parent is present in the list
		if t.ParentID != "" && parentIDs[t.ParentID] {
			continue
		}
		seen[t.ID] = true

		g := historyGroup{Task: t}
		if t.IsEpic {
			g.SubIssues = childrenOf[t.ID]
		}
		groups = append(groups, g)
	}
	return groups
}

// renderEpicProgressBar renders a compact progress bar: [##--]
// innerWidth chars inside brackets, '#' for done, '-' for remaining.
func renderEpicProgressBar(done, total, innerWidth int) string {
	if total <= 0 {
		return "[" + strings.Repeat("-", innerWidth) + "]"
	}
	filled := done * innerWidth / total
	if filled > innerWidth {
		filled = innerWidth
	}
	empty := innerWidth - filled
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", empty) + "]"
}

// renderHistory renders completed tasks history with epic-aware grouping.
// Active epics show expanded with sub-issue tree; completed epics collapse to one line.
func (m Model) renderHistory() string {
	var content strings.Builder
	tw := m.effectivePanelTotalWidth()
	iw := tw - 4

	if len(m.completedTasks) == 0 {
		content.WriteString("  No completed tasks yet")
		return renderPanel("HISTORY", content.String(), tw)
	}

	groups := m.groupedHistory()
	first := true

	for _, g := range groups {
		if g.Task.IsEpic {
			isActive := g.Task.DoneSubs < g.Task.TotalSubs
			if isActive {
				// Active epic: expanded with progress bar and sub-issues
				if !first {
					content.WriteString("\n")
				}
				first = false
				content.WriteString(renderActiveEpicLine(g.Task, iw))
				for _, sub := range g.SubIssues {
					content.WriteString("\n")
					content.WriteString(renderSubIssueLine(sub, iw))
				}
			} else {
				// Completed epic: collapsed single line with [N/N]
				if !first {
					content.WriteString("\n")
				}
				first = false
				content.WriteString(renderCompletedEpicLine(g.Task, iw))
			}
		} else {
			// Standalone task: same as before
			if !first {
				content.WriteString("\n")
			}
			first = false
			content.WriteString(renderStandaloneLine(g.Task, iw))
		}
	}

	return renderPanel("HISTORY", content.String(), tw)
}

// renderStandaloneLine renders a standalone (non-epic) task line.
// Layout: "  + GH-156  Title...                                    2m ago"
// indent(2) + icon(1) + space(1) + id(7) + space(2) + title + space(2) + timeAgo(8) = iw
// When PeakRSSMB > 0 (GH-3028), an RSS indicator ("4.2G") is appended after the time.
func renderStandaloneLine(task CompletedTask, iw int) string {
	icon, style := statusIconStyle(task.Status)
	timeAgoStr := formatTimeAgo(task.CompletedAt)

	var rssStr string
	if task.PeakRSSMB > 0 {
		rssStr = fmt.Sprintf(" %s", formatRSSMB(task.PeakRSSMB))
	}

	// Reserve space: indent(2)+icon(1)+sp(1)+id(7)+sp(2)+sp(2)+time(8)+rss = 23+len(rssStr)
	titleWidth := iw - 23 - len(rssStr)
	if titleWidth < 10 {
		titleWidth = 10
	}
	titleStr := padOrTruncate(task.Title, titleWidth)

	return fmt.Sprintf("  %s %-7s  %s  %8s%s",
		style.Render(icon),
		task.ID,
		titleStr,
		dimStyle.Render(timeAgoStr),
		dimStyle.Render(rssStr),
	)
}

// formatRSSMB formats a RSS value in MiB as a compact human-readable string.
// 512 → "512M", 2048 → "2.0G", 10240 → "10G".
func formatRSSMB(mb int) string {
	if mb < 1024 {
		return fmt.Sprintf("%dM", mb)
	}
	gb := float64(mb) / 1024.0
	if gb < 10 {
		return fmt.Sprintf("%.1fG", gb)
	}
	return fmt.Sprintf("%.0fG", gb)
}

// renderActiveEpicLine renders the parent line for an active epic.
func renderActiveEpicLine(task CompletedTask, iw int) string {
	const progressInnerWidth = 4
	// Recalculate: total = indent(2)+icon(1)+sp(1)+id(7)+sp(2)+title+sp(2)+right(rightWidth) = 65
	// title = 65 - 2 - 1 - 1 - 7 - 2 - 2 - rightWidth = 65 - 15 - rightWidth
	// Let's be precise:
	// indent(2) + icon(1) + sp(1) + id(7) + sp(2) + title + sp(1) + progress(6) + sp(1) + counts + sp(1) + time
	// We need the right side to fit. Let's use fixed columns:

	bar := renderEpicProgressBar(task.DoneSubs, task.TotalSubs, progressInnerWidth)
	counts := fmt.Sprintf("%d/%d", task.DoneSubs, task.TotalSubs)
	timeStr := task.Duration
	if timeStr == "" {
		timeStr = formatTimeAgo(task.CompletedAt)
	}

	// Right part: " [##--] 2/3   3m" — build with fixed width
	// bar(6) + sp(1) + counts(padded to 5) + sp(1) + time(padded to 5)
	rightPart := fmt.Sprintf(" %s %-5s %5s", bar, counts, timeStr)
	rightLen := len(rightPart) // plain ASCII, no ANSI

	// Title gets whatever remains
	tWidth := iw - 2 - 1 - 1 - 7 - 2 - rightLen
	if tWidth < 10 {
		tWidth = 10
	}

	titleStr := padOrTruncate(task.Title, tWidth)

	return fmt.Sprintf("  %s %-7s  %s%s",
		warningStyle.Render("*"),
		task.ID,
		titleStr,
		rightPart,
	)
}

// renderCompletedEpicLine renders a collapsed completed epic.
func renderCompletedEpicLine(task CompletedTask, iw int) string {
	counts := fmt.Sprintf("[%d/%d]", task.DoneSubs, task.TotalSubs)
	timeAgoStr := formatTimeAgo(task.CompletedAt)

	// Right part: " [N/N]    Xm ago"
	rightPart := fmt.Sprintf(" %s  %8s", counts, timeAgoStr)
	rightLen := len(rightPart)

	// Title = iw - indent(2) - icon(1) - sp(1) - id(7) - sp(2) - rightLen
	tWidth := iw - 2 - 1 - 1 - 7 - 2 - rightLen
	if tWidth < 10 {
		tWidth = 10
	}

	icon, style := statusIconStyle(task.Status)
	titleStr := padOrTruncate(task.Title, tWidth)

	return fmt.Sprintf("  %s %-7s  %s%s",
		style.Render(icon),
		task.ID,
		titleStr,
		dimStyle.Render(rightPart),
	)
}

// renderSubIssueLine renders an indented sub-issue line under an active epic.
func renderSubIssueLine(task CompletedTask, iw int) string {
	titleWidth := iw - 25 // extra 2 indent vs standalone
	icon, style := subIssueIconStyle(task.Status)

	var timeStr string
	switch task.Status {
	case "pending":
		timeStr = "--"
	case "running":
		timeStr = "now"
	default:
		timeStr = formatTimeAgo(task.CompletedAt)
	}

	titleStr := padOrTruncate(task.Title, titleWidth)

	return fmt.Sprintf("    %s %-7s  %s  %8s",
		style.Render(icon),
		task.ID,
		titleStr,
		dimStyle.Render(timeStr),
	)
}

// statusIconStyle returns the icon and style for a task status (top-level tasks).
func statusIconStyle(status string) (string, lipgloss.Style) {
	switch status {
	case "success":
		return "+", statusCompletedStyle
	case "failed":
		return "x", statusFailedStyle
	case "stalled":
		return "~", statusFailedStyle
	case "no_op":
		return "=", statusPendingStyle // TASK-358: no-change run, not a failure
	case "declined":
		return "-", statusPendingStyle // TASK-358: agent declined as unactionable
	case "rate_limited":
		return "%", statusPendingStyle // TASK-358: provider quota hit, transient
	case "infra":
		return "!", statusPendingStyle // TASK-358: plumbing/resource failure, not the work
	case "skipped":
		return ".", statusPendingStyle // TASK-358: never ran / cancelled
	case "running":
		return "~", statusRunningStyle
	default:
		return ".", statusPendingStyle
	}
}

// subIssueIconStyle returns the icon and style for a sub-issue status.
// Uses the same mapping but included for clarity/future divergence.
func subIssueIconStyle(status string) (string, lipgloss.Style) {
	return statusIconStyle(status)
}

// formatTimeAgo formats a time as relative duration
func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)
	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		mins := int(duration.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		return fmt.Sprintf("%dh ago", hours)
	}
	return t.Format("Jan 2")
}
