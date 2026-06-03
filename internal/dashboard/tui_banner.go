package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderBanner returns the avionics banner: 3 content rows wrapped with inner
// padding rows for breathing space.
//
//	╭─ PILOT ──────────────────────────────────────────────────────────╮
//	│                                                                   │
//	│ 1636/  PILOT v2.103.0      ENV STAGE      MODEL OPUS-4-7 / SONNET │
//	│                                                                   │
//	│ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ │
//	│                                                                   │
//	│ DAEMON ●  GH ●  TG ●  SLACK ○  DISCORD ○      UP 4m 12s  16:36 UTC│
//	│                                                                   │
//	╰───────────────────────────────────────────────────────────────────╯
//
// Adapter dots: ● filled (statusRunningStyle) when active this session,
// ○ empty (dimStyle) when configured but not flagged. Adapters with no
// config are not present in m.banner.adapters and don't render at all.
// pilotLogo is the ASCII art shown during splash boot.
const pilotLogo = `
   ██████╗ ██╗██╗      ██████╗ ████████╗
   ██╔══██╗██║██║     ██╔═══██╗╚══██╔══╝
   ██████╔╝██║██║     ██║   ██║   ██║
   ██╔═══╝ ██║██║     ██║   ██║   ██║
   ██║     ██║███████╗╚██████╔╝   ██║
   ╚═╝     ╚═╝╚══════╝ ╚═════╝    ╚═╝
`

// renderSplash returns the boot screen shown for the first ~1.5s of the
// dashboard session. Lamps progressively light up as splashFrame advances;
// the final frames flash READY before the splash dismisses.
func (m Model) renderSplash() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(titleStyle.Render(strings.TrimPrefix(pilotLogo, "\n")))
	sb.WriteString("\n")

	ver := m.version
	if idx := strings.Index(ver, "-"); idx > 0 {
		ver = ver[:idx]
	}
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}
	tagline := dimStyle.Render("AI THAT SHIPS YOUR TICKETS")
	verStyled := labelStyle.Render(ver)
	// 47-char wide row to roughly match the logo block.
	gap := 47 - lipgloss.Width(tagline) - lipgloss.Width(verStyled)
	if gap < 1 {
		gap = 1
	}
	sb.WriteString("   " + tagline + strings.Repeat(" ", gap) + verStyled + "\n\n")

	// BOOT block: 4 lamp lines, lit one at a time as splashFrame progresses.
	const ruleWidth = 49
	rule := dimStyle.Render(strings.Repeat("─", ruleWidth))
	sb.WriteString("   " + dimStyle.Render("BOOT ") + dimStyle.Render(strings.Repeat("─", ruleWidth-5)) + "\n")

	cfgPath := m.splash.configPath
	if cfgPath == "" {
		cfgPath = "~/.pilot/config.yaml"
	}
	adapterList := splashAdapterList(m.banner.adapters)
	if adapterList == "" {
		adapterList = dimStyle.Render("(none configured)")
	}
	model := m.banner.modelStack
	if model == "" {
		model = dimStyle.Render("(unset)")
	}
	envName := strings.ToUpper(m.banner.envName)
	if envName == "" {
		envName = dimStyle.Render("(default)")
	}

	lamps := []struct {
		label, value string
	}{
		{"config loaded", cfgPath},
		{"adapters online", adapterList},
		{"model stack", model},
		{"env", envName},
	}
	// Threshold = ceil(splashFramesTotal/2) so all 4 lamps light by mid-splash.
	litCount := m.splash.frame * len(lamps) / (splashFramesTotal / 2)
	if litCount > len(lamps) {
		litCount = len(lamps)
	}
	for i, l := range lamps {
		var dot string
		if i < litCount {
			dot = statusRunningStyle.Render("●")
		} else {
			dot = dimStyle.Render("○")
		}
		sb.WriteString(fmt.Sprintf("   %s %s    %s\n",
			dot, dimStyle.Render(padTo(l.label, 16)), l.value))
	}
	sb.WriteString("   " + rule + "\n")

	// READY footer: appears in the last 3 frames.
	if m.splash.frame >= splashFramesTotal-3 {
		sb.WriteString(strings.Repeat(" ", 45) + statusCompletedStyle.Render("READY") + "\n")
	} else {
		sb.WriteString("\n")
	}

	return sb.String()
}

// splashAdapterList renders "github · telegram · slack" from the banner's
// adapter list (configured adapters only, lowercased).
func splashAdapterList(adapters []AdapterStatus) string {
	if len(adapters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(adapters))
	for _, a := range adapters {
		parts = append(parts, strings.ToLower(a.Name))
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}

func (m Model) renderBanner() string {
	tw := m.effectivePanelTotalWidth()
	// Match the 3-space inner padding other panels use (e.g. AUTOPILOT, QUEUE).
	// buildContentLine reserves "│ " on each side (2 chars), so the content
	// receives tw - 4 cells. Reserving 2 more inside gives a 3-space gutter.
	const innerGutter = 2
	w := tw - 4 - innerGutter*2 // usable content width inside the gutter

	// --- Line 1: code/  PILOT vX.Y.Z   ENV xxx   MODEL plan / exec
	// Strip dev suffix (e.g. "-4-g14764db1-dirty") so the banner shows the
	// clean release version. The full SHA still appears in the session code
	// prefix, so no information is lost.
	ver := m.version
	if idx := strings.Index(ver, "-"); idx > 0 {
		ver = ver[:idx]
	}
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}

	leftPart := labelStyle.Render(ver)

	envSeg := ""
	if m.banner.envName != "" {
		envSeg = dimStyle.Render("ENV") + " " + statusRunningStyle.Render(strings.ToUpper(m.banner.envName))
	}

	modelSeg := ""
	if m.banner.modelStack != "" {
		modelSeg = dimStyle.Render("MODEL") + " " + labelStyle.Render(m.banner.modelStack)
	}

	line1 := joinSegmentsSpaced(w, leftPart, envSeg, modelSeg)

	// --- Line 2: tick separator
	line2 := buildTickSeparator(w)

	// --- Line 3: adapter chips (left), uptime + clock (right)
	upStr := ""
	if !m.banner.startTime.IsZero() {
		upStr = dimStyle.Render("UP") + " " + labelStyle.Render(formatDurationShort(time.Since(m.banner.startTime)))
	}
	clockStr := dimStyle.Render(time.Now().UTC().Format("15:04") + " UTC")
	rightPart := clockStr
	if upStr != "" {
		rightPart = upStr + "  " + clockStr
	}

	// Available room for chips = inner width minus right side and a small gap.
	chipsBudget := w - lipgloss.Width(rightPart) - 2

	chipsStr := buildAdapterChipsRow(m.banner.adapters, chipsBudget)

	line3 := padLeftRightLine(w, chipsStr, rightPart)

	// Wrap each line with the inner gutter so the banner aligns with sibling
	// panels (which start their content 3 chars in from the border).
	gutter := strings.Repeat(" ", innerGutter)
	wrap := func(s string) string { return gutter + s + gutter }

	pad := buildEmptyLine(tw, slatePanelStyle)
	var lines []string
	lines = append(lines, buildTopBorder("PILOT", tw, slatePanelStyle))
	lines = append(lines, pad)
	lines = append(lines, buildContentLine(wrap(line1), tw, slatePanelStyle))
	lines = append(lines, pad)
	lines = append(lines, buildContentLine(wrap(line2), tw, slatePanelStyle))
	lines = append(lines, pad)
	lines = append(lines, buildContentLine(wrap(line3), tw, slatePanelStyle))
	lines = append(lines, pad)
	lines = append(lines, buildBottomBorder(tw, slatePanelStyle))
	return strings.Join(lines, "\n")
}

// buildAdapterChipsRow packs DAEMON + per-adapter chips into a row that fits
// within `budget` visual chars. Strategy:
//  1. Always include "DAEMON ●".
//  2. Add active adapters first (●, full name).
//  3. Add inactive adapters next (○, full name) until budget is reached.
//  4. If inactive adapters remain that didn't fit, append "+N idle" summary.
func buildAdapterChipsRow(adapters []AdapterStatus, budget int) string {
	const sep = "  "

	type chipEntry struct {
		render   string
		inactive bool
	}

	daemon := chipEntry{render: dimStyle.Render("DAEMON") + " " + statusRunningStyle.Render("●")}
	chips := []chipEntry{daemon}

	for _, a := range adapters {
		if !a.Active {
			continue
		}
		chips = append(chips, chipEntry{
			render: dimStyle.Render(strings.ToUpper(a.Name)) + " " + statusRunningStyle.Render("●"),
		})
	}
	for _, a := range adapters {
		if a.Active {
			continue
		}
		chips = append(chips, chipEntry{
			render:   dimStyle.Render(strings.ToUpper(a.Name)) + " " + dimStyle.Render("○"),
			inactive: true,
		})
	}

	// First pass: include chips greedily until budget runs out.
	included := []chipEntry{}
	used := 0
	for i, c := range chips {
		extra := lipgloss.Width(c.render)
		if i > 0 {
			extra += lipgloss.Width(sep)
		}
		if used+extra > budget {
			break
		}
		included = append(included, c)
		used += extra
	}

	skippedInactive := 0
	for i := len(included); i < len(chips); i++ {
		if chips[i].inactive {
			skippedInactive++
		}
	}

	// If we dropped any inactive chips, append a "+N idle" summary, dropping
	// trailing inactive chips as needed to make room (and updating the count).
	if skippedInactive > 0 {
		for {
			summary := dimStyle.Render(fmt.Sprintf("+%d idle", skippedInactive))
			cost := lipgloss.Width(sep) + lipgloss.Width(summary)
			if used+cost <= budget {
				included = append(included, chipEntry{render: summary})
				break
			}
			// No room — drop trailing chip. If it was inactive, bump the count.
			if len(included) <= 1 {
				break // can't drop DAEMON
			}
			drop := included[len(included)-1]
			included = included[:len(included)-1]
			used -= lipgloss.Width(drop.render) + lipgloss.Width(sep)
			if drop.inactive {
				skippedInactive++
			}
		}
	}

	parts := make([]string, 0, len(included))
	for _, c := range included {
		parts = append(parts, c.render)
	}
	return strings.Join(parts, sep)
}

// joinSegmentsSpaced packs leading + middle + trailing segments into a row of
// inner width w with the leading segment left-aligned, trailing right-aligned,
// and middle segments distributed in between with even spacing. Empty segments
// are skipped.
func joinSegmentsSpaced(w int, segs ...string) string {
	var nonEmpty []string
	for _, s := range segs {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	if len(nonEmpty) == 0 {
		return strings.Repeat(" ", w)
	}
	if len(nonEmpty) == 1 {
		return padTo(nonEmpty[0], w)
	}

	// Total visual width of segments
	used := 0
	for _, s := range nonEmpty {
		used += lipgloss.Width(s)
	}
	gaps := len(nonEmpty) - 1
	free := w - used
	if free < gaps {
		// Not enough room — fall back to single-space joins.
		return padTo(strings.Join(nonEmpty, " "), w)
	}
	per := free / gaps
	rem := free % gaps

	var sb strings.Builder
	for i, s := range nonEmpty {
		sb.WriteString(s)
		if i < gaps {
			extra := 0
			if i < rem {
				extra = 1
			}
			sb.WriteString(strings.Repeat(" ", per+extra))
		}
	}
	return sb.String()
}

// padLeftRightLine packs left content left-aligned and right content
// right-aligned within width w. Truncates left if total exceeds w.
func padLeftRightLine(w int, left, right string) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw >= w {
		// Right wins on overflow; left is dropped.
		if rw >= w {
			return right
		}
		return strings.Repeat(" ", w-rw) + right
	}
	gap := w - lw - rw
	return left + strings.Repeat(" ", gap) + right
}

// padTo right-pads s with spaces to reach visual width w.
func padTo(s string, w int) string {
	visual := lipgloss.Width(s)
	if visual >= w {
		return s
	}
	return s + strings.Repeat(" ", w-visual)
}

// buildTickSeparator builds a solid horizontal rule of width w using "─".
func buildTickSeparator(w int) string {
	if w <= 0 {
		return ""
	}
	return dimStyle.Render(strings.Repeat("─", w))
}
