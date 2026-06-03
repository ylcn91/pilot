package dashboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// Risk-level styles for the findings panel. Severity escalates from sage
// (low) through amber (medium), dusty rose (high), to a bold red marker for
// release blockers so the most dangerous findings are visually unmistakable.
var (
	riskLowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7ec699")) // sage green

	riskMediumStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4a054")) // amber

	riskHighStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d48a8a")) // dusty rose

	riskBlockerStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ff5f5f")) // bright red

	riskUnknownStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e")) // mid gray
)

// releaseBlockerMarker prefixes a release-blocker finding so it stands out
// even in stripped (no-ANSI) output. Kept ASCII-only for terminal safety.
const releaseBlockerMarker = "!! "

// updateFindingsMsg replaces the model's findings slice. It mirrors the
// updateTasksMsg pattern: a value type carrying the full new slice.
type updateFindingsMsg []pilotapi.Finding

// UpdateFindings sends a fresh set of Architect findings to the TUI. It clones
// the UpdateTasks/UpdateTokens command pattern so the provider (Radar,
// Dependency-Doctor, …) can push findings through the same tea.Cmd plumbing.
func UpdateFindings(findings []pilotapi.Finding) tea.Cmd {
	return func() tea.Msg {
		return updateFindingsMsg(findings)
	}
}

// riskStyle returns the lipgloss style and the uppercased label used to render
// a given risk level. Unknown/empty levels fall back to a neutral gray so the
// panel never panics on data from a future Architect that emits new levels.
func riskStyle(r pilotapi.RiskLevel) (lipgloss.Style, string) {
	switch r {
	case pilotapi.RiskLow:
		return riskLowStyle, "LOW"
	case pilotapi.RiskMedium:
		return riskMediumStyle, "MEDIUM"
	case pilotapi.RiskHigh:
		return riskHighStyle, "HIGH"
	case pilotapi.RiskReleaseBlocker:
		return riskBlockerStyle, "BLOCKER"
	default:
		label := strings.ToUpper(strings.TrimSpace(string(r)))
		if label == "" {
			label = "UNKNOWN"
		}
		return riskUnknownStyle, label
	}
}

// renderFindings renders the FINDINGS panel: one row per finding showing a
// risk label (color-coded by RiskLevel), the title, and the file count. The
// most severe risk (release-blocker) is prefixed with a marker so it is
// distinct even without color.
func (m Model) renderFindings() string {
	tw := m.effectivePanelTotalWidth()
	iw := tw - 4 // content width inside borders/padding

	if len(m.findings) == 0 {
		return renderPanel("FINDINGS", "  "+dimStyle.Render("no findings"), tw)
	}

	var content strings.Builder
	for i, f := range m.findings {
		if i > 0 {
			content.WriteString("\n")
		}
		content.WriteString(m.renderFinding(f, iw))
	}

	return renderPanel("FINDINGS", content.String(), tw)
}

// renderFinding renders a single finding row within the given inner width.
//
// Layout (iw inner chars):
//
//	"  " + [marker] + label + "  " + title<pad> + " " + "(N files)"
func (m Model) renderFinding(f pilotapi.Finding, iw int) string {
	style, label := riskStyle(f.Risk)

	marker := ""
	if f.Risk == pilotapi.RiskReleaseBlocker {
		marker = releaseBlockerMarker
	}

	// Fixed-width label column so titles align across rows.
	labelCol := fmt.Sprintf("%-7s", label)
	renderedLabel := style.Render(marker + labelCol)

	files := fmt.Sprintf("(%d %s)", len(f.Files), pluralizeFiles(len(f.Files)))

	// Visual budget: indent(2) + marker + labelCol + gap(2) + title + gap(1) + files
	prefixLen := 2 + len(marker) + len(labelCol) + 2
	titleMax := iw - prefixLen - 1 - len(files)
	if titleMax < 5 {
		titleMax = 5
	}
	title := truncateVisual(f.Title, titleMax)

	used := prefixLen + lipgloss.Width(title) + len(files)
	pad := iw - used
	if pad < 1 {
		pad = 1
	}

	return "  " + renderedLabel + "  " + labelStyle.Render(title) +
		strings.Repeat(" ", pad) + dimStyle.Render(files)
}

// pluralizeFiles returns "file" or "files" for the given count.
func pluralizeFiles(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}
