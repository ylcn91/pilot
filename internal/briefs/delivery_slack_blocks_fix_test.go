package briefs

import (
	"testing"
)

// TestConvertSlackBlocksMalformedNoPanic proves the panic-safety fix: malformed
// block shapes (non-string "type", non-map "text", wrong "elements" element
// types, missing fields) must not panic the conversion. Before the fix the
// unchecked b["type"].(string) (and nested assertions) panicked on these.
func TestConvertSlackBlocksMalformedNoPanic(t *testing.T) {
	malformed := []map[string]interface{}{
		// "type" is not a string -> previously panicked on b["type"].(string)
		{"type": 42},
		// "type" missing entirely
		{"text": map[string]interface{}{"type": "mrkdwn", "text": "hi"}},
		// "text" present but its nested "type"/"text" are wrong types
		{"type": "section", "text": map[string]interface{}{"type": 1, "text": false}},
		// "text" is not a map at all
		{"type": "section", "text": "not-a-map"},
		// "elements" entries with wrong nested value types
		{"type": "context", "elements": []map[string]interface{}{
			{"type": 7, "text": nil},
			{},
		}},
		// nil values
		{"type": nil},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("convertSlackBlocks panicked on malformed input: %v", r)
		}
	}()

	got := convertSlackBlocks(malformed)

	// Blocks lacking a valid string "type" are skipped; the rest survive.
	// Valid-type blocks here: the two "section" blocks, the "context" block,
	// plus "type": nil and "type": 42 and the type-missing one are dropped.
	wantTypes := []string{"section", "section", "context"}
	if len(got) != len(wantTypes) {
		t.Fatalf("expected %d converted blocks, got %d: %+v", len(wantTypes), len(got), got)
	}
	for i, wt := range wantTypes {
		if got[i].Type != wt {
			t.Errorf("block %d: type = %q, want %q", i, got[i].Type, wt)
		}
	}

	// First section has a map "text" with wrong nested value types: the Text
	// object is populated but its fields degrade to empty strings, not a panic.
	textBlock := got[0] // {"type":"section","text":{"type":1,"text":false}}
	if textBlock.Text == nil {
		t.Fatal("expected Text object to be populated for a section with a map text")
	}
	if textBlock.Text.Type != "" || textBlock.Text.Text != "" {
		t.Errorf("expected malformed nested text to degrade to empty strings, got %+v", textBlock.Text)
	}

	// Second section has a non-map "text": no Text object is attached (the
	// comma-ok on the text map fails) and conversion still does not panic.
	if got[1].Text != nil {
		t.Errorf("expected nil Text for a section whose text is not a map, got %+v", got[1].Text)
	}

	// The context block's malformed elements degrade to empty TextObjects.
	ctxBlock := got[2]
	if len(ctxBlock.Elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(ctxBlock.Elements))
	}
	if ctxBlock.Elements[0].Type != "" || ctxBlock.Elements[0].Text != "" {
		t.Errorf("expected malformed element to degrade to empty strings, got %+v", ctxBlock.Elements[0])
	}
}

// TestConvertSlackBlocksWellFormed verifies the happy path is unchanged: a
// real brief's blocks convert to typed slack.Block values with text/elements
// preserved.
func TestConvertSlackBlocksWellFormed(t *testing.T) {
	brief := createTestBrief()
	blocks := NewSlackFormatter().SlackBlocks(brief)

	got := convertSlackBlocks(blocks)

	if len(got) != len(blocks) {
		t.Fatalf("expected %d blocks, got %d", len(blocks), len(got))
	}

	// Header block: type "header" with a populated text object.
	if got[0].Type != "header" {
		t.Errorf("first block type = %q, want %q", got[0].Type, "header")
	}
	if got[0].Text == nil || got[0].Text.Text == "" {
		t.Errorf("header block text not preserved: %+v", got[0].Text)
	}

	// The context (metrics) block carries elements.
	var sawElements bool
	for _, b := range got {
		if b.Type == "context" {
			if len(b.Elements) == 0 || b.Elements[0].Text == "" {
				t.Errorf("context block elements not preserved: %+v", b.Elements)
			}
			sawElements = true
		}
	}
	if !sawElements {
		t.Error("expected a context block with elements in the converted output")
	}
}
