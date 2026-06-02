package dashboard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Help footer truncation fix tests ---

func TestGitGraph_ToggleAlwaysWorks(t *testing.T) {
	// "g" should cycle gitGraphMode regardless of terminal width
	for _, width := range []int{80, 120} {
		m := Model{width: width, height: 40}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
		m = updated.(Model)
		if m.gitGraphMode != GitGraphVisible {
			t.Errorf("width=%d: gitGraphMode = %d, want %d (Full)", width, m.gitGraphMode, GitGraphVisible)
		}
	}
}

func TestHelpFooter_AlwaysShowsGraphHint(t *testing.T) {
	// "g: graph" should appear in help regardless of terminal width
	for _, width := range []int{80, 120} {
		m := Model{width: width, height: 40, gitGraphMode: GitGraphHidden}
		plain := stripANSI(m.renderHelp())
		if !strings.Contains(plain, "g: graph") {
			t.Errorf("width=%d: help should show 'g: graph', got: %q", width, plain)
		}
	}
}

func TestHelpFooter_SurvivesHeightTruncation(t *testing.T) {
	m := Model{
		width: 120, height: 10, gitGraphMode: GitGraphHidden,
		showBanner: true, showLogs: true,
		autopilotPanel: NewAutopilotPanel(nil),
	}

	view := m.View()
	lines := strings.Split(view, "\n")

	// The last line should contain help text
	lastLine := lines[len(lines)-1]
	plain := stripANSI(lastLine)
	if !strings.Contains(plain, "q: quit") {
		t.Errorf("help footer missing from last line after height truncation, got: %q", plain)
	}
}

func TestHelpFooter_VisibleWithoutTruncation(t *testing.T) {
	m := Model{
		width: 120, height: 200, gitGraphMode: GitGraphHidden,
		autopilotPanel: NewAutopilotPanel(nil),
	}

	view := m.View()
	plain := stripANSI(view)
	if !strings.Contains(plain, "q: quit") {
		t.Error("help footer should be visible when terminal is tall enough")
	}
}

// --- Responsive stacked git graph tests ---

func TestGitGraph_StackedLayoutUsesFullWidth(t *testing.T) {
	// On narrow terminal (<90 cols), graph should stack below dashboard at full terminal width
	m := Model{
		width: 80, height: 40, gitGraphMode: GitGraphVisible,
		autopilotPanel: NewAutopilotPanel(nil),
		gitGraphState: &GitGraphState{
			Lines: []GitGraphLine{
				{GraphChars: "●", SHA: "abc1234", Author: "Test", Message: "Initial commit"},
				{GraphChars: "●", SHA: "def5678", Author: "Test", Message: "Second commit"},
			},
		},
	}

	view := m.View()
	lines := strings.Split(view, "\n")

	// Find the git graph panel top border in the stacked output
	var graphBorderLine string
	for _, line := range lines {
		plain := stripANSI(line)
		if strings.Contains(plain, "GIT GRAPH") && strings.Contains(plain, "╭") {
			graphBorderLine = plain
			break
		}
	}
	if graphBorderLine == "" {
		t.Fatal("stacked graph panel not found in narrow terminal output")
	}

	// The graph panel border should span close to full terminal width (80), not panelTotalWidth (69)
	borderWidth := lipgloss.Width(graphBorderLine)
	if borderWidth <= panelTotalWidth {
		t.Errorf("stacked graph width = %d, want > %d (panelTotalWidth); should use full terminal width", borderWidth, panelTotalWidth)
	}
	if borderWidth != m.width {
		t.Errorf("stacked graph width = %d, want %d (m.width)", borderWidth, m.width)
	}
}

func TestGitGraph_SideBySideOnWideTerminal(t *testing.T) {
	// On wide terminal (≥90 cols), graph renders side-by-side
	m := Model{
		width: 120, height: 40, gitGraphMode: GitGraphVisible,
		autopilotPanel: NewAutopilotPanel(nil),
		gitGraphState: &GitGraphState{
			Lines: []GitGraphLine{
				{GraphChars: "●", SHA: "abc1234", Author: "Test", Message: "Initial commit"},
			},
		},
	}

	view := m.View()
	lines := strings.Split(view, "\n")

	// In side-by-side mode, the GIT GRAPH border should NOT be at full terminal width
	for _, line := range lines {
		plain := stripANSI(line)
		if strings.Contains(plain, "GIT GRAPH") && strings.Contains(plain, "╭") {
			borderWidth := lipgloss.Width(plain)
			if borderWidth == m.width {
				t.Errorf("side-by-side graph should not be full terminal width (%d)", m.width)
			}
			break
		}
	}
}

func TestGitGraph_StackedHelpFooterVisible(t *testing.T) {
	// Help footer must be visible at bottom even when graph is stacked
	m := Model{
		width: 75, height: 30, gitGraphMode: GitGraphVisible,
		autopilotPanel: NewAutopilotPanel(nil),
		gitGraphState: &GitGraphState{
			Lines: []GitGraphLine{
				{GraphChars: "●", SHA: "abc1234", Author: "Test", Message: "commit"},
			},
		},
	}

	view := m.View()
	lines := strings.Split(view, "\n")

	lastLine := lines[len(lines)-1]
	plain := stripANSI(lastLine)
	if !strings.Contains(plain, "q: quit") {
		t.Errorf("help footer missing from stacked layout, last line: %q", plain)
	}
}

func TestGitGraph_NarrowTerminalNotSilent(t *testing.T) {
	// On narrow terminal with graph enabled, pressing 'g' should produce visible graph output
	m := Model{
		width: 60, height: 30, gitGraphMode: GitGraphVisible,
		autopilotPanel: NewAutopilotPanel(nil),
		gitGraphState: &GitGraphState{
			Lines: []GitGraphLine{
				{GraphChars: "●", SHA: "abc1234", Author: "Test", Message: "Initial commit"},
			},
		},
	}

	view := m.View()
	plain := stripANSI(view)
	// At 60 cols stacked, auto-size picks medium (title "GIT")
	if !strings.Contains(plain, "GIT") {
		t.Error("narrow terminal (60 cols) should show stacked GIT panel, got silent/empty")
	}
}

func TestDashboardPanels_StretchInStackedMode(t *testing.T) {
	// GH-1909: In stacked mode, dashboard panels should stretch to full terminal width,
	// matching the git graph panel width for visual consistency.
	m := Model{
		width: 80, height: 40, gitGraphMode: GitGraphVisible,
		autopilotPanel: NewAutopilotPanel(nil),
		gitGraphState: &GitGraphState{
			Lines: []GitGraphLine{
				{GraphChars: "●", SHA: "abc1234", Author: "Test", Message: "Initial commit"},
			},
		},
	}

	view := m.View()
	lines := strings.Split(view, "\n")

	// Find QUEUE panel border (a dashboard panel, not the git graph)
	var queueBorderLine string
	for _, line := range lines {
		plain := stripANSI(line)
		if strings.Contains(plain, "QUEUE") && strings.Contains(plain, "╭") {
			queueBorderLine = plain
			break
		}
	}
	if queueBorderLine == "" {
		t.Fatal("QUEUE panel not found in stacked layout output")
	}

	// Dashboard panels should stretch to full terminal width (80), not stay at panelTotalWidth (69)
	borderWidth := lipgloss.Width(queueBorderLine)
	if borderWidth != m.width {
		t.Errorf("stacked QUEUE panel width = %d, want %d (full terminal width); panels should stretch in stacked mode", borderWidth, m.width)
	}

	// Also verify HISTORY panel stretches
	var historyBorderLine string
	for _, line := range lines {
		plain := stripANSI(line)
		if strings.Contains(plain, "HISTORY") && strings.Contains(plain, "╭") {
			historyBorderLine = plain
			break
		}
	}
	if historyBorderLine == "" {
		t.Fatal("HISTORY panel not found in stacked layout output")
	}
	historyWidth := lipgloss.Width(historyBorderLine)
	if historyWidth != m.width {
		t.Errorf("stacked HISTORY panel width = %d, want %d", historyWidth, m.width)
	}
}

func TestDashboardPanels_DefaultWidthWhenNoGraph(t *testing.T) {
	// When graph is hidden (no stacked mode), panels should use the default panelTotalWidth (69)
	m := Model{
		width: 120, height: 40, gitGraphMode: GitGraphHidden,
		autopilotPanel: NewAutopilotPanel(nil),
	}

	view := m.View()
	lines := strings.Split(view, "\n")

	// Find QUEUE panel border
	for _, line := range lines {
		plain := stripANSI(line)
		if strings.Contains(plain, "QUEUE") && strings.Contains(plain, "╭") {
			borderWidth := lipgloss.Width(plain)
			if borderWidth != panelTotalWidth {
				t.Errorf("default QUEUE panel width = %d, want %d (panelTotalWidth)", borderWidth, panelTotalWidth)
			}
			return
		}
	}
	t.Fatal("QUEUE panel not found in default layout output")
}
