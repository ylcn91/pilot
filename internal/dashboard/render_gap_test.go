package dashboard

import (
	"strings"
	"testing"
)

// TestRenderShimmerBar_StaggerCenters verifies that the shimmer bright spot
// center is staggered per queue offset: center = (shimmerTick + offset*3) % width.
// Adjacent queued items (offset 0 and 1) must produce distinct bright-spot
// positions for the same tick.
func TestRenderShimmerBar_StaggerCenters(t *testing.T) {
	const width = 14

	tests := []struct {
		name        string
		shimmerTick int
		offset      int
		wantCenter  int
	}{
		{"tick0 offset0", 0, 0, 0},
		{"tick0 offset1", 0, 1, 3},
		{"tick0 offset2", 0, 2, 6},
		{"tick2 offset1", 2, 1, 5},
		{"wraps modulo width", 13, 1, (13 + 3) % width}, // 16 % 14 = 2
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{metrics: metricsState{shimmerTick: tt.shimmerTick}}
			out := stripANSI(m.renderShimmerBar(width, tt.offset))

			// The bright spot rune is '▓'; it sits at the center position,
			// offset by 1 for the leading '[' bracket.
			inner := strings.TrimPrefix(out, "[")
			inner = strings.TrimSuffix(inner, "]")
			runes := []rune(inner)
			if len(runes) != width {
				t.Fatalf("inner width = %d, want %d (out=%q)", len(runes), width, out)
			}
			gotCenter := -1
			for i, r := range runes {
				if r == '▓' {
					gotCenter = i
					break
				}
			}
			if gotCenter != tt.wantCenter {
				t.Errorf("bright center = %d, want %d (out=%q)", gotCenter, tt.wantCenter, out)
			}
		})
	}
}

// TestRenderShimmerBar_AdjacentQueuedDistinct verifies that two adjacent queued
// items at the same tick render visually distinct bars (different center).
func TestRenderShimmerBar_AdjacentQueuedDistinct(t *testing.T) {
	m := Model{metrics: metricsState{shimmerTick: 0}}
	bar0 := stripANSI(m.renderShimmerBar(14, 0))
	bar1 := stripANSI(m.renderShimmerBar(14, 1))
	if bar0 == bar1 {
		t.Errorf("adjacent queued shimmer bars are identical, want distinct: %q", bar0)
	}
}

// TestRenderTasks_QueuedStaggerOffsets verifies that renderTasks assigns
// increasing shimmer offsets to consecutive queued items so their bars differ.
func TestRenderTasks_QueuedStaggerOffsets(t *testing.T) {
	m := NewModel("test")
	m.metrics.shimmerTick = 0
	m.selectedTask = -1 // no selection so neither row gets the ▸ marker
	m.tasks = []TaskDisplay{
		{ID: "GH-1", Title: "first", Status: "queued"},
		{ID: "GH-2", Title: "second", Status: "queued"},
	}

	out := stripANSI(m.renderTasks())
	lines := strings.Split(out, "\n")

	// Collect the two queued content rows (they contain the "queued" label).
	var queuedRows []string
	for _, l := range lines {
		if strings.Contains(l, "queued") {
			queuedRows = append(queuedRows, l)
		}
	}
	if len(queuedRows) != 2 {
		t.Fatalf("expected 2 queued rows, got %d: %v", len(queuedRows), queuedRows)
	}

	// Each queued row shows a position meta "#1", "#2" derived from queueOffset+1.
	if !strings.Contains(queuedRows[0], "#1") {
		t.Errorf("first queued row missing #1 meta: %q", queuedRows[0])
	}
	if !strings.Contains(queuedRows[1], "#2") {
		t.Errorf("second queued row missing #2 meta: %q", queuedRows[1])
	}

	// The shimmer portions (bracketed bars) must differ between the two rows
	// because their offsets (0 and 1) move the bright spot to distinct centers.
	bar0 := bracketedSegment(queuedRows[0])
	bar1 := bracketedSegment(queuedRows[1])
	if bar0 == "" || bar1 == "" {
		t.Fatalf("could not extract shimmer bars: %q / %q", queuedRows[0], queuedRows[1])
	}
	if bar0 == bar1 {
		t.Errorf("adjacent queued shimmer bars identical, want staggered: %q", bar0)
	}
}

// bracketedSegment returns the first "[...]" segment of a line, or "".
func bracketedSegment(line string) string {
	start := strings.Index(line, "[")
	if start < 0 {
		return ""
	}
	end := strings.Index(line[start:], "]")
	if end < 0 {
		return ""
	}
	return line[start : start+end+1]
}

// TestRenderLogs_WindowAndTruncation verifies the 10-line window cap and
// per-line visual truncation in renderLogs.
func TestRenderLogs_WindowAndTruncation(t *testing.T) {
	m := NewModel("test")

	// Seed 15 logs; only the last 10 should render.
	for i := 1; i <= 15; i++ {
		m.logs = append(m.logs, "log-"+itoa(i))
	}

	out := stripANSI(m.renderLogs())

	// The oldest 5 (log-1..log-5) must be dropped by the start = len-10 window.
	for i := 1; i <= 5; i++ {
		if strings.Contains(out, "log-"+itoa(i)+" ") || strings.Contains(out, "log-"+itoa(i)+"\n") {
			t.Errorf("log-%d should be outside the 10-line window, but rendered: %q", i, out)
		}
	}
	// The newest 10 (log-6..log-15) must all appear.
	for i := 6; i <= 15; i++ {
		if !strings.Contains(out, "log-"+itoa(i)) {
			t.Errorf("log-%d should be within the 10-line window, but missing", i)
		}
	}

	// Count content rows that carry a log entry (indented with two spaces).
	logRows := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "log-") {
			logRows++
		}
	}
	if logRows != 10 {
		t.Errorf("rendered log rows = %d, want 10", logRows)
	}
}

func TestRenderLogs_LineTruncatedToWidth(t *testing.T) {
	m := NewModel("test")
	// A log far longer than the inner content width must be truncated with "...".
	longLine := strings.Repeat("X", 500)
	m.logs = []string{longLine}

	out := stripANSI(m.renderLogs())
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncated long log to contain ellipsis, got: %q", out)
	}
	if strings.Contains(out, strings.Repeat("X", 200)) {
		t.Errorf("long log was not truncated: %q", out)
	}
}

func TestRenderLogs_Empty(t *testing.T) {
	m := NewModel("test")
	out := stripANSI(m.renderLogs())
	if !strings.Contains(out, "No logs yet") {
		t.Errorf("expected empty-state message, got: %q", out)
	}
}

// TestAddLogMsg_RingBufferCap verifies the addLogMsg handler caps m.logs at 100
// entries, dropping the oldest on overflow (ring-buffer behavior).
func TestAddLogMsg_RingBufferCap(t *testing.T) {
	m := NewModel("test")

	var model Model = m
	for i := 1; i <= 105; i++ {
		updated, _ := model.Update(addLogMsg("entry-" + itoa(i)))
		model = updated.(Model)
	}

	if len(model.logs) != 100 {
		t.Fatalf("logs len = %d, want 100 (ring-buffer cap)", len(model.logs))
	}
	// The first 5 entries should have been evicted; oldest remaining is entry-6.
	if model.logs[0] != "entry-6" {
		t.Errorf("oldest log = %q, want entry-6", model.logs[0])
	}
	if model.logs[len(model.logs)-1] != "entry-105" {
		t.Errorf("newest log = %q, want entry-105", model.logs[len(model.logs)-1])
	}
}

// itoa is a tiny local helper to avoid importing strconv just for test labels.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
