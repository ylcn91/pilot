package plane

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for sanitize.go: sanitizeWorkItemInPlace strips invisible
// Unicode format characters (ASCII smuggling vectors) from the WorkItem
// struct before it is handed to any downstream consumer (onIssue callback,
// memory store, prompt builder).
// ---------------------------------------------------------------------------

func TestSanitizeWorkItemInPlace_StripsInvisible(t *testing.T) {
	// U+200B zero-width space and U+E0041 (tag "A") — both must be stripped.
	hidden := string(rune(0x200B)) + string(rune(0xE0041))

	item := &WorkItem{
		ID:          "wi-1337",
		Name:        "Fix typo" + hidden,
		Description: "Line 2 needs fix." + hidden,
	}

	sanitizeWorkItemInPlace(item)

	if item.Name != "Fix typo" {
		t.Errorf("Name not stripped: got %q, want %q", item.Name, "Fix typo")
	}
	if item.Description != "Line 2 needs fix." {
		t.Errorf("Description not stripped: got %q, want %q",
			item.Description, "Line 2 needs fix.")
	}
}

func TestSanitizeWorkItemInPlace_NilSafe(t *testing.T) {
	// Must not panic on nil — the helper guards for nil explicitly.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sanitizeWorkItemInPlace panicked on nil: %v", r)
		}
	}()
	sanitizeWorkItemInPlace(nil)
}

func TestSanitizeWorkItemInPlace_CleanInputIsNoOp(t *testing.T) {
	// Happy path: already-clean input must pass through unchanged.
	item := &WorkItem{
		ID:          "wi-1",
		Name:        "Simple title",
		Description: "Plain body\nwith newlines\tand tabs.",
	}
	wantName, wantDesc := item.Name, item.Description

	sanitizeWorkItemInPlace(item)

	if item.Name != wantName {
		t.Errorf("clean Name mutated: got %q, want %q", item.Name, wantName)
	}
	if item.Description != wantDesc {
		t.Errorf("clean Description mutated: got %q, want %q", item.Description, wantDesc)
	}
}
