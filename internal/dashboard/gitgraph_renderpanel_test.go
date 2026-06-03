package dashboard

import (
	"strings"
	"testing"
	"time"
)

// TestRenderGitGraph_AutoSizeSmall verifies narrow width auto-selects small rendering.
func TestRenderGitGraph_AutoSizeSmall(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = panelTotalWidth + 2 + 35 // available = 35 < 40 → small
	m.height = 30
	m.gitGraph.state = &GitGraphState{
		Lines: []GitGraphLine{
			{GraphChars: "● ", SHA: "abc1234", Author: "Alice", Refs: "HEAD -> main", Message: "test commit"},
		},
	}

	got := m.renderGitGraph()
	plain := stripANSI(got)
	// Small: title "GIT", no SHA, no author
	if strings.Contains(plain, "GIT GRAPH") {
		t.Error("small auto-size should use 'GIT' title, not 'GIT GRAPH'")
	}
	if strings.Contains(plain, "abc1234") {
		t.Error("small auto-size should not contain SHA")
	}
}

// TestRenderGitGraph_AutoSizeMedium verifies medium width auto-selects medium rendering.
func TestRenderGitGraph_AutoSizeMedium(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = panelTotalWidth + 2 + 50 // available = 50, between 40-64 → medium
	m.height = 30
	m.gitGraph.state = &GitGraphState{
		Lines: []GitGraphLine{
			{GraphChars: "● ", SHA: "abc1234", Author: "Alice", Refs: "HEAD -> main", Message: "test commit"},
		},
	}

	got := m.renderGitGraph()
	plain := stripANSI(got)
	if strings.Contains(plain, "GIT GRAPH") {
		t.Error("medium auto-size should use 'GIT' title, not 'GIT GRAPH'")
	}
	// Medium has refs but no SHA
	if strings.Contains(plain, "abc1234") {
		t.Error("medium auto-size should not contain SHA")
	}
}

// TestRenderGitGraph_AutoSizeFull verifies wide width auto-selects full rendering.
func TestRenderGitGraph_AutoSizeFull(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = panelTotalWidth + 2 + 70 // available = 70 > 65 → full
	m.height = 30
	m.gitGraph.state = &GitGraphState{
		Lines: []GitGraphLine{
			{GraphChars: "● ", SHA: "abc1234", Author: "Alice", Message: "test commit"},
		},
	}

	got := m.renderGitGraph()
	plain := stripANSI(got)
	if !strings.Contains(plain, "GIT GRAPH") {
		t.Error("full auto-size should use 'GIT GRAPH' title")
	}
	if !strings.Contains(plain, "abc1234") {
		t.Error("full auto-size should contain SHA")
	}
}

// TestRenderGitGraph_Hidden verifies no output when mode is Hidden.
func TestRenderGitGraph_Hidden(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphHidden

	got := m.renderGitGraph()
	if got != "" {
		t.Errorf("renderGitGraph Hidden mode should return empty string, got %q", got)
	}
}

// TestRenderGitGraph_NarrowTerminal verifies graph is hidden when too narrow.
func TestRenderGitGraph_NarrowTerminal(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = 75 // remaining = 75 - 69 - 2 = 4, below minimum 20

	got := m.renderGitGraph()
	if got != "" {
		t.Errorf("should return empty for narrow terminal (width=%d), got non-empty", m.width)
	}
}

// TestRenderGitGraph_Loading verifies loading state renders correctly.
func TestRenderGitGraph_Loading(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.gitGraph.state = nil
	m.width = 130

	got := m.renderGitGraph()
	if got == "" {
		t.Fatal("loading state should produce non-empty output")
	}

	plain := stripANSI(got)
	if !strings.Contains(plain, "Loading") {
		t.Errorf("loading state should contain 'Loading', got:\n%s", plain)
	}
}

// TestRenderGitGraph_Error verifies error state renders correctly.
func TestRenderGitGraph_Error(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = 130
	m.gitGraph.state = &GitGraphState{
		Error:       "fatal: not a git repository",
		LastRefresh: time.Now(),
	}

	got := m.renderGitGraph()
	if got == "" {
		t.Fatal("error state should produce non-empty output")
	}

	plain := stripANSI(got)
	if !strings.Contains(plain, "fatal: not a git") {
		t.Errorf("error state should show error message, got:\n%s", plain)
	}
}

// TestRenderGitGraph_WithData verifies full rendering with commit data.
func TestRenderGitGraph_WithData(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = 140
	m.gitGraph.state = &GitGraphState{
		TotalCount:  3,
		LastRefresh: time.Now(),
		Lines: []GitGraphLine{
			{
				GraphChars: "● ",
				SHA:        "7eb8da1",
				Author:     "Alice Smith",
				Refs:       "HEAD -> main",
				Message:    "feat: add git graph",
			},
			{GraphChars: "├╌╮"},
			{
				GraphChars: "│ ●",
				SHA:        "a1b2c3d",
				Author:     "Bob Jones",
				Message:    "feat: add tests",
			},
		},
	}

	got := m.renderGitGraph()
	if got == "" {
		t.Fatal("renderGitGraph with data returned empty string")
	}

	plain := stripANSI(got)

	if !strings.Contains(plain, "GIT GRAPH") {
		t.Error("missing 'GIT GRAPH' panel title")
	}
	if !strings.Contains(plain, "feat: add git graph") {
		t.Errorf("missing commit message in output:\n%s", plain)
	}
	if !strings.Contains(plain, "[1-3 of 3]") {
		t.Errorf("missing scroll indicator, got:\n%s", plain)
	}
}

// TestRenderGitGraph_FocusedBorder verifies both focused and unfocused panels render.
// Note: color differences require a TTY; here we only verify both render non-empty
// and contain the correct panel structure.
func TestRenderGitGraph_FocusedBorder(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = 140
	m.gitGraph.state = &GitGraphState{Lines: []GitGraphLine{{GraphChars: "● ", SHA: "abc1234", Message: "test"}}}

	// Focused panel
	m.gitGraph.focus = true
	focused := m.renderGitGraph()
	if focused == "" {
		t.Error("focused panel should render non-empty")
	}

	// Unfocused panel
	m.gitGraph.focus = false
	unfocused := m.renderGitGraph()
	if unfocused == "" {
		t.Error("unfocused panel should render non-empty")
	}

	// Both should contain the panel structure
	focusedPlain := stripANSI(focused)
	if !strings.Contains(focusedPlain, "GIT GRAPH") {
		t.Error("focused panel missing GIT GRAPH title")
	}
	unfocusedPlain := stripANSI(unfocused)
	if !strings.Contains(unfocusedPlain, "GIT GRAPH") {
		t.Error("unfocused panel missing GIT GRAPH title")
	}
}

// TestGitGraphState_ScrollIndicator verifies scroll indicator shows correct range.
func TestGitGraphState_ScrollIndicator(t *testing.T) {
	m := NewModel("test")
	m.gitGraph.mode = GitGraphVisible
	m.width = 130
	m.gitGraph.scroll = 10

	lines := make([]GitGraphLine, 50)
	for i := range lines {
		lines[i] = GitGraphLine{
			GraphChars: "● ",
			SHA:        "abc1234",
			Message:    "commit message",
		}
	}
	m.gitGraph.state = &GitGraphState{
		TotalCount: 50,
		Lines:      lines,
	}

	got := m.renderGitGraph()
	plain := stripANSI(got)

	// scroll=10, visible=30, end=min(10+30, 50)=40
	// indicator: [11-40 of 50]
	if !strings.Contains(plain, "[11-40 of 50]") {
		t.Errorf("scroll indicator incorrect, got:\n%s", plain)
	}
}
