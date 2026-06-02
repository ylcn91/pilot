package dashboard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// makeKey creates a tea.KeyMsg for the given string (single char or named key).
func makeKey(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		// Single-rune key (e.g., "g", "j", "k")
		runes := []rune(s)
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
	}
}

// TestTranslateGraphChars verifies git graph characters are mapped to our symbol set.
func TestTranslateGraphChars(t *testing.T) {
	// '*' → '●'
	got := TranslateGraphChars("*")
	if !strings.Contains(got, "●") {
		t.Errorf("* should become ●, got %q", got)
	}

	// '|' → '│' (or '├' after post-processing)
	got = TranslateGraphChars("|")
	if got != "│" {
		t.Errorf("| should become │, got %q", got)
	}

	// '-' → '╌'
	got = TranslateGraphChars("-")
	if got != "╌" {
		t.Errorf("- should become ╌, got %q", got)
	}

	// Spaces are unchanged
	got = TranslateGraphChars("  ")
	if got != "  " {
		t.Errorf("spaces should be unchanged, got %q", got)
	}
}

// TestTranslateGraphChars_JunctionReplacement verifies branch-off/merge-back junctions.
func TestTranslateGraphChars_JunctionReplacement(t *testing.T) {
	// "|\\" in git output: | becomes │, \ becomes ╮, then │╮ → ├╌╮
	got := TranslateGraphChars(`|\`)
	if !strings.Contains(got, "├") || !strings.Contains(got, "╮") {
		t.Errorf("branch-off `|\\` should produce ├...╮, got %q", got)
	}

	// "|/" in git output: │╯ → ├╌╯
	got = TranslateGraphChars("|/")
	if !strings.Contains(got, "├") || !strings.Contains(got, "╯") {
		t.Errorf("merge-back `|/` should produce ├...╯, got %q", got)
	}
}

// TestParseGitGraphOutput verifies raw git log lines are parsed correctly.
func TestParseGitGraphOutput(t *testing.T) {
	// Commit line:    graph_chars + NUL + sha|author|refs|message
	// Connector line: graph_chars only (no NUL)
	raw := "* \x007eb8da1|Alice Smith|HEAD -> main|feat: add dashboard\n" +
		"|\n" +
		"* \x00a1b2c3d|Bob Jones||fix: handle nil"

	lines := ParseGitGraphOutput(raw)

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Line 0: commit line
	l0 := lines[0]
	if l0.SHA != "7eb8da1" {
		t.Errorf("line 0 SHA = %q, want %q", l0.SHA, "7eb8da1")
	}
	if l0.Message != "feat: add dashboard" {
		t.Errorf("line 0 Message = %q, want %q", l0.Message, "feat: add dashboard")
	}
	if l0.Refs != "HEAD -> main" {
		t.Errorf("line 0 Refs = %q, want %q", l0.Refs, "HEAD -> main")
	}

	// Line 1: pure connector
	l1 := lines[1]
	if l1.SHA != "" {
		t.Errorf("connector line should have empty SHA, got %q", l1.SHA)
	}
	if l1.Message != "" {
		t.Errorf("connector line should have empty Message, got %q", l1.Message)
	}

	// Line 2: commit with empty refs
	l2 := lines[2]
	if l2.SHA != "a1b2c3d" {
		t.Errorf("line 2 SHA = %q, want %q", l2.SHA, "a1b2c3d")
	}
	if l2.Refs != "" {
		t.Errorf("line 2 Refs should be empty, got %q", l2.Refs)
	}
}

// TestParseGitGraphOutput_Empty verifies empty input is handled gracefully.
func TestParseGitGraphOutput_Empty(t *testing.T) {
	lines := ParseGitGraphOutput("")
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for empty input, got %d", len(lines))
	}
}

// TestAbbreviateAuthor verifies author name abbreviation.
func TestAbbreviateAuthor(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Alice", "Alice"},
		{"Al", "Al"},
		{"Alice Smith", "A. Smith"},
		{"First Middle Last", "F. Last"},
		{"VeryLongNameNoSpaces", "VeryLongNa"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := AbbreviateAuthor(tt.input)
			if got != tt.want {
				t.Errorf("AbbreviateAuthor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestColorizeRefs verifies refs are styled correctly.
func TestColorizeRefs(t *testing.T) {
	tests := []struct {
		name    string
		refs    string
		wantSub string
		empty   bool
	}{
		{"empty refs", "", "", true},
		{"HEAD ref", "HEAD -> main", "HEAD -> main", false},
		{"branch ref", "refs/heads/pilot/GH-123", "pilot/GH-123", false},
		{"tag ref", "tag: refs/tags/v1.0.0", "v1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorizeRefs(tt.refs)
			plain := stripANSI(got)
			if tt.empty {
				if got != "" {
					t.Errorf("colorizeRefs(%q) = %q, want empty", tt.refs, got)
				}
				return
			}
			if !strings.Contains(plain, tt.wantSub) {
				t.Errorf("colorizeRefs(%q) plain = %q, want substring %q", tt.refs, plain, tt.wantSub)
			}
		})
	}
}
