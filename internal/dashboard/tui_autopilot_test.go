package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ylcn91/pilot/internal/autopilot"
)

// --- GH-2455: avionics redesign tests ---

func TestRenderBanner(t *testing.T) {
	m := NewModel("2.102.3")
	start := time.Now().Add(-90 * time.Minute) // 1h30m ago
	m.SetBannerMeta("prod", "opus:plan | sonnet:exec", nil, start)
	m.SetBannerAdapters([]AdapterStatus{
		{Name: "GH", Active: true},
		{Name: "SLACK", Active: false},
	})

	out := m.renderBanner()

	// Banner uppercases env, MODEL/ENV labels, and adapter names.
	for _, want := range []string{"v2.102.3", "PROD", "opus:plan", "UTC", "GH", "SLACK", "DAEMON"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderBanner() missing %q\nout:\n%s", want, out)
		}
	}

	// Verify each rendered line is exactly panelTotalWidth visual chars wide.
	for i, line := range strings.Split(out, "\n") {
		w := lipgloss.Width(line)
		if w != panelTotalWidth {
			t.Errorf("renderBanner() line %d: visual width = %d, want %d  (line: %q)", i, w, panelTotalWidth, line)
		}
	}
}

func TestRenderBannerDefaults(t *testing.T) {
	// Banner renders without metadata set (env defaults to "─", no adapters, no uptime).
	m := NewModel("1.0.0")
	out := m.renderBanner()

	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("renderBanner() missing version")
	}
	if !strings.Contains(out, "UTC") {
		t.Errorf("renderBanner() missing UTC clock")
	}

	for _, line := range strings.Split(out, "\n") {
		w := lipgloss.Width(line)
		if w != panelTotalWidth {
			t.Errorf("renderBanner() default: line width = %d, want %d", w, panelTotalWidth)
		}
	}
}

func TestAutopilotPanelDisabled(t *testing.T) {
	p := NewAutopilotPanel(nil)
	out := p.View()

	if !strings.Contains(out, "Disabled") {
		t.Errorf("AutopilotPanel(nil).View() missing 'Disabled'")
	}
	for _, line := range strings.Split(out, "\n") {
		w := lipgloss.Width(line)
		if w != panelTotalWidth {
			t.Errorf("AutopilotPanel disabled: line width = %d, want %d", w, panelTotalWidth)
		}
	}
}

func TestRenderAutopilotRailPositions(t *testing.T) {
	// Rail uses glyph-per-node: ✓ (done) / ◐◓◑◒ (current, animated) / ○ (pending) / ✗ (failed).
	// Spinner index 0 → ◐ for the in-progress node.
	tests := []struct {
		stage    autopilot.PRStage
		ciStatus autopilot.CIStatus
		tick     int
		wantDone int    // count of ✓
		wantPend int    // count of ○
		wantFail int    // count of ✗
		nodeName string // must appear in rail
	}{
		{autopilot.StageWaitingCI, autopilot.CIPending, 0, 0, 4, 0, "ci"},
		{autopilot.StageMerging, autopilot.CISuccess, 0, 2, 2, 0, "merge"},
		{autopilot.StagePostMergeCI, autopilot.CISuccess, 0, 3, 1, 0, "tag"},
		{autopilot.StageReleasing, autopilot.CISuccess, 0, 4, 0, 0, "release"},
		// CI failure: ✗ on ci node
		{autopilot.StageCIFailed, autopilot.CIFailure, 0, 0, 4, 1, "ci"},
		// Pipeline failure (non-CI)
		{autopilot.StageFailed, autopilot.CIPending, 0, 0, 4, 1, "ci"},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			out := renderAutopilotRail(tt.stage, tt.ciStatus, tt.tick)
			plain := stripANSI(out)
			if got := strings.Count(plain, "✓"); got != tt.wantDone {
				t.Errorf("stage %s: ✓ count = %d, want %d (rail: %q)", tt.stage, got, tt.wantDone, plain)
			}
			if got := strings.Count(plain, "○"); got != tt.wantPend {
				t.Errorf("stage %s: ○ count = %d, want %d (rail: %q)", tt.stage, got, tt.wantPend, plain)
			}
			if got := strings.Count(plain, "✗"); got != tt.wantFail {
				t.Errorf("stage %s: ✗ count = %d, want %d (rail: %q)", tt.stage, got, tt.wantFail, plain)
			}
			if !strings.Contains(plain, tt.nodeName) {
				t.Errorf("stage %s: rail missing node %q (rail: %q)", tt.stage, tt.nodeName, plain)
			}
			// Old glyph must not appear
			if strings.Contains(plain, "●") {
				t.Errorf("stage %s: rail contains deprecated ● glyph: %q", tt.stage, plain)
			}
			// No fake progress bars
			if strings.Contains(plain, "[█") || strings.Contains(plain, "[░") {
				t.Errorf("stage %s: rail contains fake progress bar chars: %q", tt.stage, plain)
			}
		})
	}
}

func TestRenderAutopilotRailSpinner(t *testing.T) {
	// Each tick value should produce a different spinner rune for the active node.
	spinnerRunes := []string{"◐", "◓", "◑", "◒"}
	for tick, want := range spinnerRunes {
		out := renderAutopilotRail(autopilot.StageWaitingCI, autopilot.CIPending, tick)
		plain := stripANSI(out)
		if !strings.Contains(plain, want) {
			t.Errorf("tick=%d: expected spinner rune %q not found in rail: %q", tick, want, plain)
		}
	}
}

func TestAutopilotPanelView_AllStates(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		ctl         autopilotController
		wantLines   int    // total output lines (borders included)
		wantContain string // substring in plain text
		wantAbsent  string // substring that must NOT appear
	}{
		{
			name:        "disabled",
			ctl:         nil,
			wantLines:   5, // top border + empty + content + empty + bottom border
			wantContain: "Disabled",
		},
		{
			name:        "idle",
			ctl:         newFakeCtl(nil, 3, nil),
			wantLines:   5, // top border + empty + content + empty + bottom border
			wantContain: "idle · no active PR",
			wantAbsent:  "STATE",
		},
		{
			name: "ci-running steady state",
			ctl: newFakeCtl([]*autopilot.PRState{{
				PRNumber:  2565,
				PRTitle:   "fix(upgrade): atomic binary replacement",
				Stage:     autopilot.StageWaitingCI,
				CIStatus:  autopilot.CIRunning,
				CreatedAt: now.Add(-90 * time.Second),
			}}, 3, nil),
			wantLines:   6, // border + empty + line1 + line2 + empty + border
			wantContain: "#2565",
			wantAbsent:  "[█",
		},
		{
			name: "ci-failed with error",
			ctl: newFakeCtl([]*autopilot.PRState{{
				PRNumber:  2565,
				PRTitle:   "fix(upgrade): atomic binary replacement",
				Stage:     autopilot.StageFailed,
				CIStatus:  autopilot.CIFailure,
				Error:     "TestInstallToBinaryPath_Cleanup failed · linux-amd64",
				CreatedAt: now.Add(-4 * time.Minute),
			}}, 3, map[int]int{2565: 2}),
			wantLines:   7, // border + empty + line1 + line2 + line3(error) + empty + border
			wantContain: "↳",
			wantAbsent:  "STATE",
		},
		{
			name: "rebase in progress",
			ctl: newFakeCtl([]*autopilot.PRState{{
				PRNumber:  2565,
				PRTitle:   "fix(upgrade): atomic binary replacement",
				Stage:     autopilot.StageAwaitApproval,
				CIStatus:  autopilot.CISuccess,
				CreatedAt: now.Add(-3 * time.Minute),
			}}, 3, nil),
			wantLines:   6, // border + empty + line1 + line2 + empty + border
			wantContain: "✓",
			wantAbsent:  "[░",
		},
		{
			name: "released",
			ctl: newFakeCtl([]*autopilot.PRState{{
				PRNumber:  2565,
				PRTitle:   "fix(upgrade): atomic binary replacement",
				Stage:     autopilot.StageReleasing,
				CIStatus:  autopilot.CISuccess,
				CreatedAt: now.Add(-10 * time.Minute),
			}}, 3, nil),
			wantLines:   6, // border + empty + line1 + line2 + empty + border
			wantContain: "release",
			wantAbsent:  "STATE",
		},
		{
			name: "failed no error message",
			ctl: newFakeCtl([]*autopilot.PRState{{
				PRNumber:  2565,
				PRTitle:   "fix(upgrade): atomic binary replacement",
				Stage:     autopilot.StageFailed,
				CIStatus:  autopilot.CIPending,
				Error:     "",
				CreatedAt: now.Add(-2 * time.Minute),
			}}, 3, nil),
			wantLines:  6, // border + empty + line1 + line2 + empty + border (no error line)
			wantAbsent: "↳",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &AutopilotPanel{
				controller: tt.ctl,
				panelWidth: panelTotalWidth,
				tick:       0,
			}
			out := p.View()
			plain := stripANSI(out)
			lines := strings.Split(out, "\n")

			if len(lines) != tt.wantLines {
				t.Errorf("line count = %d, want %d\noutput:\n%s", len(lines), tt.wantLines, plain)
			}

			if tt.wantContain != "" && !strings.Contains(plain, tt.wantContain) {
				t.Errorf("output missing %q\nplain:\n%s", tt.wantContain, plain)
			}
			if tt.wantAbsent != "" && strings.Contains(plain, tt.wantAbsent) {
				t.Errorf("output must not contain %q\nplain:\n%s", tt.wantAbsent, plain)
			}

			// No fake progress bars in any state
			if strings.Contains(plain, "[█") || strings.Contains(plain, "[░") {
				t.Errorf("output contains fake progress bar chars\nplain:\n%s", plain)
			}

			// Every line must be panelTotalWidth wide
			for i, line := range lines {
				w := lipgloss.Width(line)
				if w != panelTotalWidth {
					t.Errorf("line %d visual width = %d, want %d: %q", i, w, panelTotalWidth, line)
				}
			}
		})
	}
}

func TestAutopilotPanelTick_RotatesSpinner(t *testing.T) {
	ctl := newFakeCtl([]*autopilot.PRState{{
		PRNumber:  100,
		PRTitle:   "test PR",
		Stage:     autopilot.StageWaitingCI,
		CIStatus:  autopilot.CIPending,
		CreatedAt: time.Now(),
	}}, 3, nil)

	spinner := []string{"◐", "◓", "◑", "◒"}
	for tick, want := range spinner {
		p := &AutopilotPanel{controller: ctl, panelWidth: panelTotalWidth, tick: tick}
		plain := stripANSI(p.View())
		if !strings.Contains(plain, want) {
			t.Errorf("tick=%d: spinner rune %q not found in output:\n%s", tick, want, plain)
		}
	}
}
