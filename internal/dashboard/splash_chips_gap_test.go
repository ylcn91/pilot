package dashboard

import (
	"strings"
	"testing"
)

// countLitLamps counts the "●" lamp glyphs in the rendered splash boot block.
// The DAEMON-style filled dot uses the same rune, but the splash only emits "●"
// for lit lamps and "○" for unlit ones.
func countLitLamps(s string) int {
	return strings.Count(s, "●")
}

// TestRenderSplash_LampProgression verifies the litCount formula:
// litCount = splashFrame * len(lamps) / (splashFramesTotal/2), clamped to len.
// With len(lamps)=4 and splashFramesTotal=10, the divisor is 5.
func TestRenderSplash_LampProgression(t *testing.T) {
	tests := []struct {
		frame   int
		wantLit int
	}{
		{0, 0},  // 0*4/5 = 0
		{1, 0},  // 4/5 = 0
		{2, 1},  // 8/5 = 1
		{3, 2},  // 12/5 = 2
		{4, 3},  // 16/5 = 3
		{5, 4},  // 20/5 = 4 (all lit)
		{6, 4},  // clamped to len(lamps)
		{9, 4},  // clamped
		{20, 4}, // far past total stays clamped
	}

	for _, tt := range tests {
		m := NewModel("v2.0.0")
		m.splash.frame = tt.frame
		out := stripANSI(m.renderSplash())
		if got := countLitLamps(out); got != tt.wantLit {
			t.Errorf("frame %d: lit lamps = %d, want %d\n%s", tt.frame, got, tt.wantLit, out)
		}
	}
}

// TestRenderSplash_ReadyFooterTiming verifies the READY footer only appears in
// the last 3 frames (splashFrame >= splashFramesTotal-3 == 7).
func TestRenderSplash_ReadyFooterTiming(t *testing.T) {
	tests := []struct {
		frame     int
		wantReady bool
	}{
		{0, false},
		{6, false},
		{7, true},
		{8, true},
		{9, true},
	}

	for _, tt := range tests {
		m := NewModel("v2.0.0")
		m.splash.frame = tt.frame
		out := stripANSI(m.renderSplash())
		hasReady := strings.Contains(out, "READY")
		if hasReady != tt.wantReady {
			t.Errorf("frame %d: READY present = %v, want %v", tt.frame, hasReady, tt.wantReady)
		}
	}
}

// TestBuildAdapterChipsRow_AllFit verifies that with a generous budget all
// chips render and no "+N idle" summary is appended.
func TestBuildAdapterChipsRow_AllFit(t *testing.T) {
	adapters := []AdapterStatus{
		{Name: "github", Active: true},
		{Name: "slack", Active: false},
	}
	out := stripANSI(buildAdapterChipsRow(adapters, 200))

	if !strings.Contains(out, "DAEMON") {
		t.Errorf("expected DAEMON chip, got %q", out)
	}
	if !strings.Contains(out, "GITHUB") {
		t.Errorf("expected GITHUB chip, got %q", out)
	}
	if !strings.Contains(out, "SLACK") {
		t.Errorf("expected SLACK chip, got %q", out)
	}
	if strings.Contains(out, "idle") {
		t.Errorf("did not expect +N idle summary when all chips fit: %q", out)
	}
}

// TestBuildAdapterChipsRow_IdleFallback verifies the budget-overflow path:
// when inactive adapters cannot all fit, a "+N idle" summary is appended and
// the count reflects the dropped inactive chips.
func TestBuildAdapterChipsRow_IdleFallback(t *testing.T) {
	// Three inactive adapters with a tight budget that fits DAEMON plus the
	// summary but not all the inactive chips.
	adapters := []AdapterStatus{
		{Name: "slack", Active: false},
		{Name: "discord", Active: false},
		{Name: "telegram", Active: false},
	}

	// "DAEMON ●" is 8 visual chars. Give just enough room for DAEMON + a
	// "+N idle" summary but not for the three inactive name chips.
	out := stripANSI(buildAdapterChipsRow(adapters, 20))

	if !strings.Contains(out, "DAEMON") {
		t.Errorf("DAEMON must always be present: %q", out)
	}
	if !strings.Contains(out, "idle") {
		t.Fatalf("expected +N idle fallback summary, got %q", out)
	}
	// All three inactive adapters were dropped, so the count is 3.
	if !strings.Contains(out, "+3 idle") {
		t.Errorf("expected +3 idle, got %q", out)
	}
}

// TestBuildAdapterChipsRow_ChipDropLoop verifies the drop loop: when the
// summary itself does not fit, trailing chips are dropped (and the inactive
// count bumped) until the summary fits, while DAEMON is never dropped.
func TestBuildAdapterChipsRow_ChipDropLoop(t *testing.T) {
	adapters := []AdapterStatus{
		{Name: "github", Active: true},   // active, included first
		{Name: "slack", Active: false},   // inactive
		{Name: "discord", Active: false}, // inactive
	}

	// Budget large enough to greedily include DAEMON + GITHUB + at least one
	// inactive chip in the first pass, but then the summary forces dropping a
	// trailing chip. We pick a width where DAEMON+GITHUB fit but adding the
	// summary requires dropping a trailing inactive chip.
	out := stripANSI(buildAdapterChipsRow(adapters, 24))

	if !strings.Contains(out, "DAEMON") {
		t.Errorf("DAEMON must survive the drop loop: %q", out)
	}
	if !strings.Contains(out, "idle") {
		t.Errorf("expected an idle summary after dropping trailing chips: %q", out)
	}
}

// TestBuildAdapterChipsRow_DaemonNotDroppedByLoop verifies the drop loop never
// drops DAEMON: with a budget that fits only DAEMON ("DAEMON ●" = 8 visual
// chars) the inactive chips and their summary can't fit, yet DAEMON survives
// because the loop guards `len(included) <= 1`.
func TestBuildAdapterChipsRow_DaemonNotDroppedByLoop(t *testing.T) {
	adapters := []AdapterStatus{
		{Name: "slack", Active: false},
		{Name: "discord", Active: false},
	}
	out := stripANSI(buildAdapterChipsRow(adapters, 8))
	if !strings.Contains(out, "DAEMON") {
		t.Errorf("DAEMON must survive the drop loop when it is the only chip that fits: %q", out)
	}
}
