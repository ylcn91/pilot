package asana

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for sanitize.go: sanitizeTaskInPlace strips invisible Unicode
// format characters (ASCII smuggling vectors) from the Task struct before
// it is handed to any downstream consumer (onTask callback, memory store,
// prompt builder).
// ---------------------------------------------------------------------------

func TestSanitizeTaskInPlace_StripsInvisible(t *testing.T) {
	// U+200B zero-width space and U+E0041 (tag "A") — both must be stripped.
	hidden := string(rune(0x200B)) + string(rune(0xE0041))

	task := &Task{
		GID:       "1234567890",
		Name:      "Fix typo" + hidden,
		Notes:     "Line 2 needs fix." + hidden,
		HTMLNotes: "<p>See screenshot" + hidden + "</p>",
	}

	sanitizeTaskInPlace(task)

	if task.Name != "Fix typo" {
		t.Errorf("Name not stripped: got %q, want %q", task.Name, "Fix typo")
	}
	if task.Notes != "Line 2 needs fix." {
		t.Errorf("Notes not stripped: got %q, want %q", task.Notes, "Line 2 needs fix.")
	}
	if task.HTMLNotes != "<p>See screenshot</p>" {
		t.Errorf("HTMLNotes not stripped: got %q, want %q",
			task.HTMLNotes, "<p>See screenshot</p>")
	}
}

func TestSanitizeTaskInPlace_NilSafe(t *testing.T) {
	// Must not panic on nil — the helper guards for nil explicitly.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sanitizeTaskInPlace panicked on nil: %v", r)
		}
	}()
	sanitizeTaskInPlace(nil)
}

func TestSanitizeTaskInPlace_CleanInputIsNoOp(t *testing.T) {
	// Happy path: already-clean input must pass through unchanged.
	task := &Task{
		GID:       "1",
		Name:      "Simple title",
		Notes:     "Plain body\nwith newlines\tand tabs.",
		HTMLNotes: "<p>Plain body</p>",
	}
	wantName, wantNotes, wantHTML := task.Name, task.Notes, task.HTMLNotes

	sanitizeTaskInPlace(task)

	if task.Name != wantName {
		t.Errorf("clean Name mutated: got %q, want %q", task.Name, wantName)
	}
	if task.Notes != wantNotes {
		t.Errorf("clean Notes mutated: got %q, want %q", task.Notes, wantNotes)
	}
	if task.HTMLNotes != wantHTML {
		t.Errorf("clean HTMLNotes mutated: got %q, want %q", task.HTMLNotes, wantHTML)
	}
}
