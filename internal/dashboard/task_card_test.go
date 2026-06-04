package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderTaskCard_NarrowDoesNotBlankFailed(t *testing.T) {
	m := NewModel("test")
	m.metrics.card = MetricsCardData{
		Succeeded: 1508, Failed: 234,
		NoOp: 120, Infra: 305, Skipped: 81, RateLimited: 34, Stalled: 10,
	}

	out := m.renderTaskCard(23)

	if !strings.Contains(out, "234 failed") {
		t.Errorf("narrow card dropped failed headline; got:\n%s", out)
	}
	if !strings.Contains(out, "1508 succeeded") {
		t.Errorf("narrow card dropped succeeded line; got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 23 {
			t.Errorf("line width = %d, want 23: %q", w, line)
		}
	}
}

func TestRenderTaskCard_WideShowsBreakdown(t *testing.T) {
	m := NewModel("test")
	m.metrics.card = MetricsCardData{Succeeded: 1508, Failed: 234, NoOp: 120, Infra: 305}

	out := m.renderTaskCard(90)

	for _, want := range []string{"234 failed", "120 no-op", "305 infra"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide card missing %q; got:\n%s", want, out)
		}
	}
}

func TestTruncateVisualStyled(t *testing.T) {
	styled := statusFailedStyle.Render("✗ 234 failed") +
		statusPendingStyle.Render(" (120 no-op · 305 infra · 81 skipped)")

	out := truncateVisual(styled, 17)

	if w := lipgloss.Width(out); w > 17 {
		t.Errorf("visible width = %d, want <= 17", w)
	}
	if !strings.Contains(out, "234 failed") {
		t.Errorf("truncation dropped leading visible text; got %q", out)
	}
}
