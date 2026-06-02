package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/dashboard"
)

// TestGetGitGraph_DefaultLimit verifies that passing limit=0 falls back to 100
// and that the returned GitGraphData mirrors dashboard.GitGraphState fields.
func TestGetGitGraph_DefaultLimit(t *testing.T) {
	// Use "." as project path (current dir is inside a git repo during tests).
	state := dashboard.FetchGitGraph(".", 100)
	if state == nil {
		t.Skip("git not available in test environment")
	}

	app := &App{}
	// limit=0 should default to 100 — same result as explicit 100.
	got := app.GetGitGraph(0)
	if got.TotalCount != state.TotalCount {
		t.Errorf("TotalCount mismatch: got %d, want %d", got.TotalCount, state.TotalCount)
	}
	if len(got.Lines) != len(state.Lines) {
		t.Errorf("Lines length mismatch: got %d, want %d", len(got.Lines), len(state.Lines))
	}
}

// TestGetGitGraph_LinesMapping verifies each GitGraphLine field is copied correctly.
func TestGetGitGraph_LinesMapping(t *testing.T) {
	state := dashboard.FetchGitGraph(".", 5)
	if state == nil || len(state.Lines) == 0 {
		t.Skip("no git commits available in test environment")
	}

	app := &App{}
	got := app.GetGitGraph(5)

	for i, want := range state.Lines {
		if i >= len(got.Lines) {
			t.Fatalf("missing line at index %d", i)
		}
		gl := got.Lines[i]
		if gl.GraphChars != want.GraphChars {
			t.Errorf("line[%d].GraphChars = %q, want %q", i, gl.GraphChars, want.GraphChars)
		}
		if gl.SHA != want.SHA {
			t.Errorf("line[%d].SHA = %q, want %q", i, gl.SHA, want.SHA)
		}
		if gl.Message != want.Message {
			t.Errorf("line[%d].Message = %q, want %q", i, gl.Message, want.Message)
		}
	}
}
