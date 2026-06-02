package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/ylcn91/pilot/internal/autopilot"
)

// stripANSI removes ANSI escape sequences for snapshot comparison.
// We compare visual content, not terminal styling.
func stripANSI(s string) string {
	// Simple ANSI escape stripper: \x1b[...m
	result := strings.Builder{}
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// assertPanelLineWidths checks that every line in the panel output has
// the expected visual width (panelTotalWidth = 69).
func assertPanelLineWidths(t *testing.T, output string) {
	t.Helper()
	for i, line := range strings.Split(output, "\n") {
		w := lipgloss.Width(line)
		if w != panelTotalWidth {
			t.Errorf("line %d visual width = %d, want %d: %q", i, w, panelTotalWidth, line)
		}
	}
}

// fakeAutopilotCtl implements autopilotController for testing without a real Controller.
type fakeAutopilotCtl struct {
	prs      []*autopilot.PRState
	cfg      *autopilot.Config
	failures map[int]int
}

func (f *fakeAutopilotCtl) GetActivePRs() []*autopilot.PRState { return f.prs }
func (f *fakeAutopilotCtl) Config() *autopilot.Config          { return f.cfg }
func (f *fakeAutopilotCtl) GetPRFailures(n int) int            { return f.failures[n] }

func newFakeCtl(prs []*autopilot.PRState, maxFailures int, failures map[int]int) *fakeAutopilotCtl {
	if failures == nil {
		failures = map[int]int{}
	}
	return &fakeAutopilotCtl{
		prs:      prs,
		cfg:      &autopilot.Config{MaxFailures: maxFailures},
		failures: failures,
	}
}

func newUpgradeModel(t *testing.T) Model {
	t.Helper()
	m := NewModel("v2.100.0")
	m.width = 120
	m.height = 40
	// Seed update info so renderUpdateNotification can reference version strings.
	updated, _ := m.Update(updateAvailableMsg{
		CurrentVersion: "v2.100.0",
		LatestVersion:  "v2.101.0",
	})
	return updated.(Model)
}
