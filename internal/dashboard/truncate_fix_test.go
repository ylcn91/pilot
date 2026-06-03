package dashboard

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestTruncateStringByVisualWidth proves truncateString measures by lipgloss
// visual width (delegating to truncateVisual) rather than raw byte length, so
// multi-byte Unicode is not mis-counted or sliced mid-rune.
func TestTruncateStringByVisualWidth(t *testing.T) {
	// 8 accented runes; each is 2 bytes in UTF-8 (16 bytes total) but 1 visual
	// column each. A byte-based truncation at maxLen=10 would wrongly cut after
	// ~7 bytes (mid/early), producing the wrong glyph count and risking an
	// invalid rune split. Visual-width truncation keeps it ASCII-correct.
	s := "éééééééé"

	got := truncateString(s, 10)

	// Width must never exceed the requested budget.
	if w := lipgloss.Width(got); w > 10 {
		t.Fatalf("truncateString(%q, 10) visual width = %d, want <= 10", s, w)
	}

	// The result must still be valid UTF-8 (no mid-rune byte slice).
	for i, r := range got {
		if r == '�' {
			t.Fatalf("truncateString produced replacement char (mid-rune split) at byte %d in %q", i, got)
		}
	}

	// Visual width of the source is 8, which fits in 10, so it must be
	// returned unchanged. A byte-based implementation (len = 16 > 10) would
	// have truncated it.
	if got != s {
		t.Fatalf("truncateString(%q, 10) = %q, want unchanged (fits by visual width)", s, got)
	}
}

// TestTruncateStringDelegatesToVisual confirms truncateString and
// truncateVisual produce identical output, including the small-budget path
// (maxLen <= 3) that the old byte-based implementation would have panicked on.
func TestTruncateStringDelegatesToVisual(t *testing.T) {
	cases := []struct {
		s      string
		maxLen int
	}{
		{"fix(upgrade): atomic binary replacement", 20},
		{"ééééééééééééééé", 10},
		{"short", 2},
		{"日本語のタイトル", 8},
	}
	for _, c := range cases {
		if got, want := truncateString(c.s, c.maxLen), truncateVisual(c.s, c.maxLen); got != want {
			t.Errorf("truncateString(%q, %d) = %q, want %q (must delegate to truncateVisual)", c.s, c.maxLen, got, want)
		}
	}
}
