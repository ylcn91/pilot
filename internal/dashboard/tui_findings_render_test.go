package dashboard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// --- renderFindings output content ---

func TestRenderFindings_EmptyState(t *testing.T) {
	m := NewModel("test")
	output := m.renderFindings()

	assertPanelLineWidths(t, output)

	plain := stripANSI(output)
	if !strings.Contains(plain, "FINDINGS") {
		t.Error("missing FINDINGS panel title")
	}
	if !strings.Contains(plain, "no findings") {
		t.Error("empty state should render 'no findings'")
	}
}

func TestRenderFindings_ContainsTitlesAndRiskLabels(t *testing.T) {
	m := NewModel("test")
	m.findings = sampleFindings()

	output := m.renderFindings()
	assertPanelLineWidths(t, output)

	plain := stripANSI(output)

	// Every risk label must appear.
	for _, label := range []string{"BLOCKER", "HIGH", "MEDIUM", "LOW"} {
		if !strings.Contains(plain, label) {
			t.Errorf("rendered output missing risk label %q\n%s", label, plain)
		}
	}

	// Titles must appear (allow truncation by checking a leading substring).
	wantTitleFragments := []string{
		"Unbounded goroutine",
		"Missing context",
		"Config default",
		"Redundant slice",
	}
	for _, frag := range wantTitleFragments {
		if !strings.Contains(plain, frag) {
			t.Errorf("rendered output missing title fragment %q\n%s", frag, plain)
		}
	}
}

func TestRenderFindings_FileCount(t *testing.T) {
	m := NewModel("test")
	m.findings = []pilotapi.Finding{
		{Title: "Two files", Risk: pilotapi.RiskHigh, Files: []string{"a.go", "b.go"}},
		{Title: "One file", Risk: pilotapi.RiskLow, Files: []string{"c.go"}},
		{Title: "No files", Risk: pilotapi.RiskMedium, Files: nil},
	}

	plain := stripANSI(m.renderFindings())

	if !strings.Contains(plain, "(2 files)") {
		t.Errorf("expected '(2 files)' for two-file finding\n%s", plain)
	}
	if !strings.Contains(plain, "(1 file)") {
		t.Errorf("expected '(1 file)' singular for one-file finding\n%s", plain)
	}
	if !strings.Contains(plain, "(0 files)") {
		t.Errorf("expected '(0 files)' for zero-file finding\n%s", plain)
	}
}

// --- Release-blocker is visually distinct ---

func TestRenderFinding_ReleaseBlockerMarker(t *testing.T) {
	m := NewModel("test")

	blocker := pilotapi.Finding{
		Title: "Critical leak",
		Risk:  pilotapi.RiskReleaseBlocker,
		Files: []string{"x.go"},
	}
	low := pilotapi.Finding{
		Title: "Minor nit",
		Risk:  pilotapi.RiskLow,
		Files: []string{"y.go"},
	}

	blockerRow := stripANSI(m.renderFinding(blocker, panelInnerWidth))
	lowRow := stripANSI(m.renderFinding(low, panelInnerWidth))

	// The release-blocker marker must appear on the blocker row and NOT on
	// the low row — distinct even with ANSI stripped.
	if !strings.Contains(blockerRow, strings.TrimSpace(releaseBlockerMarker)) {
		t.Errorf("blocker row missing marker %q: %q", releaseBlockerMarker, blockerRow)
	}
	if strings.Contains(lowRow, strings.TrimSpace(releaseBlockerMarker)) {
		t.Errorf("low row should not contain blocker marker: %q", lowRow)
	}
}

func TestRenderFinding_ReleaseBlockerStyled(t *testing.T) {
	// Force a color profile so styles emit ANSI escapes regardless of whether
	// the test runner has a TTY (lipgloss otherwise downgrades to plain text).
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := NewModel("test")
	blocker := pilotapi.Finding{
		Title: "Critical leak",
		Risk:  pilotapi.RiskReleaseBlocker,
		Files: []string{"x.go"},
	}

	// Raw (un-stripped) output must carry an ANSI escape, proving the row is
	// styled (color-coded) rather than plain text.
	raw := m.renderFinding(blocker, panelInnerWidth)
	if !strings.Contains(raw, "\x1b[") {
		t.Errorf("blocker row is not styled (no ANSI escape): %q", raw)
	}

	// The bold red blocker style differs from the sage low style; render a
	// known label with each and confirm the escape sequences differ.
	blockerStyle, _ := riskStyle(pilotapi.RiskReleaseBlocker)
	lowStyle, _ := riskStyle(pilotapi.RiskLow)
	if blockerStyle.Render("X") == lowStyle.Render("X") {
		t.Error("release-blocker style is indistinguishable from low-risk style")
	}
}

// --- riskStyle classification ---

func TestRiskStyle_Labels(t *testing.T) {
	cases := []struct {
		risk      pilotapi.RiskLevel
		wantLabel string
	}{
		{pilotapi.RiskLow, "LOW"},
		{pilotapi.RiskMedium, "MEDIUM"},
		{pilotapi.RiskHigh, "HIGH"},
		{pilotapi.RiskReleaseBlocker, "BLOCKER"},
	}
	for _, tc := range cases {
		_, label := riskStyle(tc.risk)
		if label != tc.wantLabel {
			t.Errorf("riskStyle(%q) label = %q, want %q", tc.risk, label, tc.wantLabel)
		}
	}
}

func TestRiskStyle_UnknownFallsBack(t *testing.T) {
	// Empty risk → UNKNOWN label, neutral style, no panic.
	_, label := riskStyle(pilotapi.RiskLevel(""))
	if label != "UNKNOWN" {
		t.Errorf("empty risk label = %q, want UNKNOWN", label)
	}

	// A non-canonical risk from a future Architect is upcased, not dropped.
	_, label2 := riskStyle(pilotapi.RiskLevel("catastrophic"))
	if label2 != "CATASTROPHIC" {
		t.Errorf("unknown risk label = %q, want CATASTROPHIC", label2)
	}
}

// --- Worst case: long title and many files must not overflow the panel ---

func TestRenderFindings_LongTitleNoOverflow(t *testing.T) {
	m := NewModel("test")
	m.findings = []pilotapi.Finding{
		{
			Title: strings.Repeat("very-long-title-segment ", 20),
			Risk:  pilotapi.RiskReleaseBlocker,
			Files: make([]string, 999),
		},
	}

	output := m.renderFindings()
	// renderPanel guarantees every line is exactly panelTotalWidth wide.
	assertPanelLineWidths(t, output)
}

func TestRenderFindings_ManyFindingsAllRendered(t *testing.T) {
	m := NewModel("test")
	var fs []pilotapi.Finding
	for i := 0; i < 25; i++ {
		fs = append(fs, pilotapi.Finding{
			Title: "Finding number " + string(rune('A'+i%26)),
			Risk:  pilotapi.RiskMedium,
			Files: []string{"f.go"},
		})
	}
	m.findings = fs

	output := m.renderFindings()
	assertPanelLineWidths(t, output)

	plain := stripANSI(output)
	// All 25 findings produce content rows; the panel adds 4 chrome lines
	// (top border, top pad, bottom pad, bottom border).
	lineCount := strings.Count(output, "\n") + 1
	if lineCount < 25+4 {
		t.Errorf("expected at least %d lines for 25 findings, got %d", 25+4, lineCount)
	}
	if !strings.Contains(plain, "MEDIUM") {
		t.Error("medium-risk label missing from many-findings render")
	}
}

// --- View integration: findings panel appears only when toggled on ---

func TestView_FindingsPanelVisibleWhenPresent(t *testing.T) {
	m := NewModel("test")
	m.width = 120
	m.height = 60
	m.findings = sampleFindings()

	plain := stripANSI(m.View())
	if !strings.Contains(plain, "FINDINGS") {
		t.Error("FINDINGS panel should appear in View() when findings present")
	}
}

func TestView_FindingsPanelHiddenWhenToggledOff(t *testing.T) {
	m := NewModel("test")
	m.width = 120
	m.height = 60
	m.findings = sampleFindings()
	m.showFindings = false

	plain := stripANSI(m.View())
	if strings.Contains(plain, "FINDINGS") {
		t.Error("FINDINGS panel should be hidden when showFindings is false")
	}
}

func TestView_FindingsPanelHiddenWhenEmpty(t *testing.T) {
	m := NewModel("test")
	m.width = 120
	m.height = 60
	// No findings set.

	plain := stripANSI(m.View())
	if strings.Contains(plain, "FINDINGS") {
		t.Error("FINDINGS panel should not appear when there are no findings")
	}
}

// --- Toggle key 'f' flips showFindings and requests repaint ---

func TestUpdate_FindingsToggleKey(t *testing.T) {
	m := NewModel("test")
	if !m.showFindings {
		t.Fatal("showFindings should default to true")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	mm := updated.(Model)
	if mm.showFindings {
		t.Error("'f' key should toggle showFindings off")
	}
	if cmd == nil {
		t.Error("'f' toggle should request a repaint (ClearScreen)")
	}

	updated2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if !updated2.(Model).showFindings {
		t.Error("second 'f' press should toggle showFindings back on")
	}
}

// --- Help footer advertises the findings key ---

func TestRenderHelp_AdvertisesFindingsKey(t *testing.T) {
	m := NewModel("test")
	m.width = 200 // wide enough that help is not truncated
	help := stripANSI(m.renderHelp())
	if !strings.Contains(help, "f: findings") {
		t.Errorf("help footer should advertise 'f: findings', got %q", help)
	}
}

func TestNewModel_FindingsDefaults(t *testing.T) {
	m := NewModel("test")
	if !m.showFindings {
		t.Error("NewModel should default showFindings to true")
	}
	if len(m.findings) != 0 {
		t.Errorf("NewModel should start with no findings, got %d", len(m.findings))
	}
}
