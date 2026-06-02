package dashboard

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// formatCompact formats a number in compact form: 0, 999, 1.0K, 57.3K, 1.2M.
func formatCompact(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

// normalizeToSparkline scales float64 values to 0-8 range for sparkline rendering.
// Left-pads with zeros if fewer values than width. Each returned int maps to a sparkBlocks index.
func normalizeToSparkline(values []float64, width int) []int {
	result := make([]int, width)
	if len(values) == 0 {
		return result
	}

	// Left-pad: place values at the right end
	offset := width - len(values)
	if offset < 0 {
		// More values than width — take the last `width` values
		values = values[len(values)-width:]
		offset = 0
	}

	// Find min/max for scaling
	minVal := values[0]
	maxVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	span := maxVal - minVal
	if span == 0 {
		// All values identical
		level := 1 // baseline for all-zero
		if values[0] > 0 {
			level = 4 // midpoint for uniform non-zero
		}
		for i := range values {
			result[offset+i] = level
		}
		return result
	}

	for i, v := range values {
		// Scale to 1-8 (reserve 0 for padding, 1 = visible baseline)
		normalized := (v - minVal) / span * 7
		level := int(math.Round(normalized)) + 1
		if v == 0 {
			level = 1 // visible baseline for zero values
		}
		if level < 1 {
			level = 1
		}
		if level > 8 {
			level = 8
		}
		result[offset+i] = level
	}

	return result
}

// renderSparkline maps int levels to sparkBlocks rune chars.
// Appends pulsing indicator (•) when pulsing=true, space otherwise.
// Total visual width equals ciw chars.
func renderSparkline(levels []int, pulsing bool, ciw int) string {
	var b strings.Builder
	// sparkline data chars = ciw - 1 (for pulsing indicator)
	dataWidth := ciw - 1

	// Render levels (take last dataWidth values, or pad left)
	start := 0
	if len(levels) > dataWidth {
		start = len(levels) - dataWidth
	}

	// Left-pad if needed
	for i := 0; i < dataWidth-len(levels)+start; i++ {
		b.WriteRune(sparkBlocks[0])
	}

	for i := start; i < len(levels); i++ {
		idx := levels[i]
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		b.WriteRune(sparkBlocks[idx])
	}

	if pulsing {
		b.WriteRune('•')
	} else {
		b.WriteRune(' ')
	}

	return b.String()
}

// --- Mini-card builder helpers ---

// miniCardEmptyLine returns a bordered empty line at exact cw width.
func miniCardEmptyLine(cw int) string {
	border := borderStyle.Render("│")
	return border + strings.Repeat(" ", cw-2) + border
}

// miniCardContentLine returns a bordered content line with 2-char padding each side.
func miniCardContentLine(content string, cw int) string {
	ciw := cw - 6 // inner width = card width minus borders and padding
	adjusted := padOrTruncate(content, ciw)
	border := borderStyle.Render("│")
	return border + "  " + adjusted + "  " + border
}

// miniCardHeaderLine returns a header with TITLE left-aligned and VALUE right-aligned.
func miniCardHeaderLine(title, value string, ciw int) string {
	styledTitle := titleStyle.Render(strings.ToUpper(title))
	titleWidth := lipgloss.Width(styledTitle)
	valueWidth := lipgloss.Width(value)
	gap := ciw - titleWidth - valueWidth
	if gap < 1 {
		gap = 1
	}
	return styledTitle + strings.Repeat(" ", gap) + value
}

// buildMiniCard assembles a full bordered mini-card.
func buildMiniCard(title, value, detail1, detail2, sparkline string, cw int) string {
	dashCount := cw - 2
	top := borderStyle.Render("╭" + strings.Repeat("─", dashCount) + "╮")
	bottom := borderStyle.Render("╰" + strings.Repeat("─", dashCount) + "╯")

	lines := []string{
		top,
		miniCardEmptyLine(cw),
		miniCardContentLine(miniCardHeaderLine(title, value, cw-6), cw),
		miniCardEmptyLine(cw),
		miniCardContentLine(detail1, cw),
		miniCardContentLine(detail2, cw),
		miniCardEmptyLine(cw),
		miniCardContentLine(sparkline, cw),
		miniCardEmptyLine(cw),
		bottom,
	}
	return strings.Join(lines, "\n")
}

// --- Card renderers ---

// renderTokenCard renders the TOKENS mini-card with the given card width.
func (m Model) renderTokenCard(cw int) string {
	ciw := cw - 6
	value := titleStyle.Render(formatCompact(m.metricsCard.TotalTokens))
	detail1 := dimStyle.Render(fmt.Sprintf("↑ %s input", formatCompact(m.metricsCard.InputTokens)))
	detail2 := dimStyle.Render(fmt.Sprintf("↓ %s output", formatCompact(m.metricsCard.OutputTokens)))

	// Convert int64 history to float64
	floats := make([]float64, len(m.metricsCard.TokenHistory))
	for i, v := range m.metricsCard.TokenHistory {
		floats[i] = float64(v)
	}
	levels := normalizeToSparkline(floats, ciw-1)
	spark := statusRunningStyle.Render(renderSparkline(levels, m.sparklineTick, ciw))

	return buildMiniCard("tokens", value, detail1, detail2, spark, cw)
}

// renderCostCard renders the COST mini-card with the given card width.
func (m Model) renderCostCard(cw int) string {
	ciw := cw - 6
	value := costStyle.Render(fmt.Sprintf("$%.2f", m.metricsCard.TotalCostUSD))
	costPerTask := m.metricsCard.CostPerTask
	detail1 := dimStyle.Render(fmt.Sprintf("~$%.2f/task", costPerTask))
	detail2 := ""

	levels := normalizeToSparkline(m.metricsCard.CostHistory, ciw-1)
	spark := statusRunningStyle.Render(renderSparkline(levels, m.sparklineTick, ciw))

	return buildMiniCard("cost", value, detail1, detail2, spark, cw)
}

// nonFailureSuffix builds the muted " (N no-op · M infra · …)" breakdown suffix
// for the QUEUE card, omitting any zero buckets. Returns "" when all are zero.
// On narrow cards the line truncates after the "✗ N failed" headline, which is
// always preserved. TASK-358: these are non-failure terminal outcomes split out
// of "failed".
func nonFailureSuffix(c MetricsCardData) string {
	buckets := []struct {
		n     int
		label string
	}{
		{c.NoOp, "no-op"},
		{c.Infra, "infra"},
		{c.Skipped, "skipped"},
		{c.RateLimited, "rate-limited"},
		{c.Stalled, "stalled"},
		{c.Declined, "declined"},
	}
	var parts []string
	for _, b := range buckets {
		if b.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", b.n, b.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " · ") + ")"
}

// renderTaskCard renders the QUEUE mini-card with the given card width.
// Value shows current queue depth (pending + running), not lifetime totals.
func (m Model) renderTaskCard(cw int) string {
	ciw := cw - 6
	value := fmt.Sprintf("%d", len(m.tasks))
	detail1 := statusCompletedStyle.Render(fmt.Sprintf("✓ %d succeeded", m.metricsCard.Succeeded))
	// TASK-358: "failed" counts genuine failures only. Non-failure terminal
	// outcomes (no-op / stalled / declined) are shown as a muted suffix so the
	// numbers reconcile and a no-op is no longer miscounted as a failure.
	detail2 := statusFailedStyle.Render(fmt.Sprintf("✗ %d failed", m.metricsCard.Failed))
	if suffix := nonFailureSuffix(m.metricsCard); suffix != "" {
		detail2 += statusPendingStyle.Render(suffix)
	}

	// Convert int history to float64
	floats := make([]float64, len(m.metricsCard.TaskHistory))
	for i, v := range m.metricsCard.TaskHistory {
		floats[i] = float64(v)
	}
	levels := normalizeToSparkline(floats, ciw-1)
	spark := statusRunningStyle.Render(renderSparkline(levels, m.sparklineTick, ciw))

	return buildMiniCard("queue", value, detail1, detail2, spark, cw)
}

// renderMetricsCards renders all three mini-cards side by side.
func (m Model) renderMetricsCards() string {
	epw := m.effectivePanelTotalWidth()
	cw := epw / 3
	remainder := epw - 3*cw
	// Distribute remainder as gaps between the 3 cards (2 gaps)
	gap1 := remainder / 2
	gap2 := remainder - gap1
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderTokenCard(cw), strings.Repeat(" ", gap1),
		m.renderCostCard(cw), strings.Repeat(" ", gap2),
		m.renderTaskCard(cw))
}
