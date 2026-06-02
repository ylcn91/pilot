package linear

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for sanitize.go: sanitizeIssueInPlace strips invisible Unicode
// format characters (ASCII smuggling vectors) from the Issue struct before
// it is handed to any downstream consumer (onIssue callback, memory store,
// prompt builder).
// ---------------------------------------------------------------------------

func TestSanitizeIssueInPlace_StripsInvisible(t *testing.T) {
	// U+200B zero-width space and U+E0041 (tag "A") — both must be stripped.
	hidden := string(rune(0x200B)) + string(rune(0xE0041))

	issue := &Issue{
		Identifier:  "ENG-42",
		Title:       "Fix typo" + hidden,
		Description: "Line 2 needs fix." + hidden,
	}

	sanitizeIssueInPlace(issue)

	if issue.Title != "Fix typo" {
		t.Errorf("Title not stripped: got %q, want %q", issue.Title, "Fix typo")
	}
	if issue.Description != "Line 2 needs fix." {
		t.Errorf("Description not stripped: got %q, want %q",
			issue.Description, "Line 2 needs fix.")
	}
}

func TestSanitizeIssueInPlace_NilSafe(t *testing.T) {
	// Must not panic on nil — the helper guards for nil explicitly.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sanitizeIssueInPlace panicked on nil: %v", r)
		}
	}()
	sanitizeIssueInPlace(nil)
}

func TestSanitizeIssueInPlace_CleanInputIsNoOp(t *testing.T) {
	// Happy path: already-clean input must pass through unchanged.
	issue := &Issue{
		Identifier:  "ENG-1",
		Title:       "Simple title",
		Description: "Plain body\nwith newlines\tand tabs.",
	}
	wantTitle, wantDesc := issue.Title, issue.Description

	sanitizeIssueInPlace(issue)

	if issue.Title != wantTitle {
		t.Errorf("clean Title mutated: got %q, want %q", issue.Title, wantTitle)
	}
	if issue.Description != wantDesc {
		t.Errorf("clean Description mutated: got %q, want %q", issue.Description, wantDesc)
	}
}
