package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ylcn91/pilot/internal/autopilot"
)

// autopilotController is the subset of autopilot.Controller used by AutopilotPanel.
// Defined as an interface so tests can inject fakes without a real Controller.
type autopilotController interface {
	GetActivePRs() []*autopilot.PRState
	Config() *autopilot.Config
	GetPRFailures(prNumber int) int
}

// AutopilotPanel displays autopilot status in the dashboard.
type AutopilotPanel struct {
	controller autopilotController
	panelWidth int // dynamic panel width, set before View()
	tick       int // increments per 1s animation tick, drives ◐ rotation
}

// NewAutopilotPanel creates an autopilot panel.
func NewAutopilotPanel(controller *autopilot.Controller) *AutopilotPanel {
	if controller == nil {
		return &AutopilotPanel{controller: nil, panelWidth: panelTotalWidth}
	}
	return &AutopilotPanel{controller: controller, panelWidth: panelTotalWidth}
}

// SetTick updates the animation tick counter (called from the parent model's tickMsg handler).
func (p *AutopilotPanel) SetTick(t int) { p.tick = t }

// View renders the autopilot panel (GH-2620 variant A redesign).
// Uses renderPanel so the card has the same 1-line top/bottom padding as QUEUE/HISTORY/LOGS.
// Idle: 5 lines (border, empty, content, empty, border).
// Active: 6 lines (+ PR identity + pipeline rail).
// Failed: 7 lines (+ error reason on line 3).
func (p *AutopilotPanel) View() string {
	tw := p.panelWidth
	if tw < panelTotalWidth {
		tw = panelTotalWidth
	}
	inner := tw - 4 // content width: tw minus 2 borders and 2 padding spaces

	if p.controller == nil {
		return renderPanel("AUTOPILOT", "  Disabled", tw)
	}

	prs := p.controller.GetActivePRs()
	if len(prs) == 0 {
		return renderPanel("AUTOPILOT", "  "+dimStyle.Render("idle · no active PR"), tw)
	}

	pr := prs[0]

	// Line 1: "  #NNNN  {title}{padding}{age}"
	age := p.formatDuration(time.Since(pr.CreatedAt))
	prefix1 := fmt.Sprintf("  #%d  ", pr.PRNumber)
	prefix1Len := lipgloss.Width(prefix1)
	ageLen := len(age)
	titleMaxLen := inner - prefix1Len - ageLen - 1 // 1 space before age
	if titleMaxLen < 5 {
		titleMaxLen = 5
	}
	title := truncateString(pr.PRTitle, titleMaxLen)
	pad1 := inner - prefix1Len - len(title) - ageLen
	if pad1 < 1 {
		pad1 = 1
	}
	line1 := prefix1 + title + strings.Repeat(" ", pad1) + age

	// Line 2: "  {rail}{padding}{N/M[ ⟲]}"
	cfg := p.controller.Config()
	maxFailures := cfg.MaxFailures
	if maxFailures <= 0 {
		maxFailures = 5
	}
	failures := p.controller.GetPRFailures(pr.PRNumber)

	rail := renderAutopilotRail(pr.Stage, pr.CIStatus, p.tick)

	retryNum := fmt.Sprintf("%d/%d", failures, maxFailures)
	var retryStr string
	if failures > 0 {
		retryStr = dimStyle.Render(retryNum) + " " + warningStyle.Render("⟲")
	} else {
		retryStr = dimStyle.Render(retryNum) + " "
	}

	pad2 := inner - 2 - lipgloss.Width(rail) - lipgloss.Width(retryStr)
	if pad2 < 1 {
		pad2 = 1
	}
	line2 := "  " + rail + strings.Repeat(" ", pad2) + retryStr

	lines := []string{line1, line2}

	// Line 3 (conditional): "  ↳ {truncated error}" — only on failure with message
	if pr.Stage == autopilot.StageFailed && pr.Error != "" {
		const errPrefix = "  ↳ "
		errMax := inner - len(errPrefix)
		if errMax < 5 {
			errMax = 5
		}
		lines = append(lines, errPrefix+truncateString(pr.Error, errMax))
	}

	// Overflow: additional active PRs
	if len(prs) > 1 {
		lines = append(lines, fmt.Sprintf("  + %d more PR(s)", len(prs)-1))
	}

	return renderPanel("AUTOPILOT", strings.Join(lines, "\n"), tw)
}

// pipelineStagePosition maps a PRStage to its 0-based position in the 5-node rail.
func pipelineStagePosition(stage autopilot.PRStage) int {
	switch stage {
	case autopilot.StagePRCreated, autopilot.StageWaitingCI,
		autopilot.StageCIPassed, autopilot.StageCIFailed:
		return 0
	case autopilot.StageAwaitApproval, autopilot.StageReviewRequested:
		return 1
	case autopilot.StageMerging, autopilot.StageMerged:
		return 2
	case autopilot.StagePostMergeCI:
		return 3
	case autopilot.StageReleasing:
		return 4
	case autopilot.StageFailed:
		return 0
	}
	return 0
}

// renderAutopilotRail renders the 5-node pipeline rail with glyph-per-node status.
// Glyphs: ✓ done (sage), ◐◓◑◒ in-progress animated (steel blue), ○ pending (gray), ✗ failed (rose).
// tick drives the spinner rotation; ciStatus allows the ci node to show ✗ independently of stage.
// Format example for stage=releasing:
//
//	✓ ci ── ✓ rebase ── ✓ merge ── ✓ tag ── ◐ release
func renderAutopilotRail(stage autopilot.PRStage, ciStatus autopilot.CIStatus, tick int) string {
	nodes := []string{"ci", "rebase", "merge", "tag", "release"}
	spinner := []rune{'◐', '◓', '◑', '◒'}
	pos := pipelineStagePosition(stage)

	var sb strings.Builder
	for i, name := range nodes {
		var glyph string
		var glyphStyle, nameStyle lipgloss.Style

		switch {
		case i == 0 && ciStatus == autopilot.CIFailure:
			// CI check failed — show ✗ on the ci node regardless of current stage
			glyph = "✗"
			glyphStyle, nameStyle = statusFailedStyle, statusFailedStyle
		case stage == autopilot.StageFailed && i == pos:
			// Pipeline failed at current position — show ✗
			glyph = "✗"
			glyphStyle, nameStyle = statusFailedStyle, statusFailedStyle
		case i < pos:
			glyph = "✓"
			glyphStyle, nameStyle = statusCompletedStyle, statusCompletedStyle
		case i == pos:
			glyph = string(spinner[tick%4])
			glyphStyle, nameStyle = statusRunningStyle, titleStyle
		default:
			glyph = "○"
			glyphStyle, nameStyle = dimStyle, dimStyle
		}

		sb.WriteString(glyphStyle.Render(glyph))
		sb.WriteString(" ")
		sb.WriteString(nameStyle.Render(name))
		if i < len(nodes)-1 {
			sb.WriteString(dimStyle.Render(" ── "))
		}
	}
	return sb.String()
}

// formatDurationShort formats a duration compactly (e.g., "2m", "1h30m").
func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// formatDuration wraps formatDurationShort for AutopilotPanel methods.
func (p *AutopilotPanel) formatDuration(d time.Duration) string {
	return formatDurationShort(d)
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
