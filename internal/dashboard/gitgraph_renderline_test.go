package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestRenderGraphLineFull verifies full-mode rendering width and content.
func TestRenderGraphLineFull(t *testing.T) {
	line := GitGraphLine{
		GraphChars: "● ",
		Refs:       "HEAD -> main",
		Message:    "feat: add git graph panel",
		Author:     "Alice Smith",
		SHA:        "7eb8da1",
	}

	width := 80
	got := renderGraphLineFull(line, width)

	if got == "" {
		t.Error("renderGraphLineFull returned empty string")
	}

	// Visual width should not exceed target width (with small tolerance for ANSI)
	visualWidth := lipgloss.Width(got)
	if visualWidth > width+2 {
		t.Errorf("renderGraphLineFull width = %d, want <= %d", visualWidth, width+2)
	}

	// Should contain the commit message
	plain := stripANSI(got)
	if !strings.Contains(plain, "feat: add git graph") {
		t.Errorf("missing commit message in full line: %q", plain)
	}
}

// TestRenderGraphLineFull_Connector verifies connector lines in full mode.
func TestRenderGraphLineFull_Connector(t *testing.T) {
	line := GitGraphLine{
		GraphChars: "├╌╮",
		SHA:        "", // no commit data
	}

	width := 80
	got := renderGraphLineFull(line, width)

	plain := stripANSI(got)
	// Should contain the branch junction characters
	if !strings.Contains(plain, "╮") {
		t.Errorf("connector line should contain ╮, got %q", plain)
	}
	// Should be padded to width
	visualWidth := lipgloss.Width(got)
	if visualWidth != width {
		t.Errorf("connector line visual width = %d, want %d", visualWidth, width)
	}
}

// TestRenderGraphLineSmall verifies small-mode rendering (graph + message only).
func TestRenderGraphLineSmall(t *testing.T) {
	line := GitGraphLine{
		GraphChars: "● ",
		Refs:       "HEAD -> main",
		Message:    "feat: add git graph panel",
		Author:     "Alice Smith",
		SHA:        "7eb8da1",
	}

	width := 28
	got := renderGraphLineSmall(line, width)

	if got == "" {
		t.Error("renderGraphLineSmall returned empty string")
	}

	plain := stripANSI(got)
	// Should contain message
	if !strings.Contains(plain, "feat:") {
		t.Errorf("missing commit message in small line: %q", plain)
	}
	// Should NOT contain SHA or author
	if strings.Contains(plain, "7eb8da1") {
		t.Errorf("small line should not contain SHA: %q", plain)
	}
	if strings.Contains(plain, "Alice") {
		t.Errorf("small line should not contain author: %q", plain)
	}
	// Should NOT contain refs
	if strings.Contains(plain, "HEAD") {
		t.Errorf("small line should not contain refs: %q", plain)
	}
}

// TestRenderGraphLineSmall_Connector verifies connector lines in small mode.
func TestRenderGraphLineSmall_Connector(t *testing.T) {
	line := GitGraphLine{GraphChars: "├╌╮"}
	width := 28
	got := renderGraphLineSmall(line, width)
	visualWidth := lipgloss.Width(got)
	if visualWidth != width {
		t.Errorf("connector visual width = %d, want %d", visualWidth, width)
	}
}

// TestRenderGraphLineMedium verifies medium-mode rendering (graph + refs + message).
func TestRenderGraphLineMedium(t *testing.T) {
	line := GitGraphLine{
		GraphChars: "● ",
		Refs:       "HEAD -> main",
		Message:    "feat: add git graph panel",
		Author:     "Alice Smith",
		SHA:        "7eb8da1",
	}

	width := 46
	got := renderGraphLineMedium(line, width)

	if got == "" {
		t.Error("renderGraphLineMedium returned empty string")
	}

	plain := stripANSI(got)
	// Should contain message
	if !strings.Contains(plain, "feat:") {
		t.Errorf("missing commit message in medium line: %q", plain)
	}
	// Should contain refs
	if !strings.Contains(plain, "HEAD") {
		t.Errorf("missing refs in medium line: %q", plain)
	}
	// Should NOT contain SHA or author
	if strings.Contains(plain, "7eb8da1") {
		t.Errorf("medium line should not contain SHA: %q", plain)
	}
	if strings.Contains(plain, "Alice") {
		t.Errorf("medium line should not contain author: %q", plain)
	}
}

// TestRenderGraphLineMedium_NoRefs verifies medium mode without refs.
func TestRenderGraphLineMedium_NoRefs(t *testing.T) {
	line := GitGraphLine{
		GraphChars: "● ",
		Message:    "fix: handle nil pointer",
		SHA:        "a1b2c3d",
	}

	width := 46
	got := renderGraphLineMedium(line, width)
	plain := stripANSI(got)
	if !strings.Contains(plain, "fix: handle nil") {
		t.Errorf("missing message: %q", plain)
	}
}
