package main

import (
	"encoding/json"
	"testing"
)

func TestParseID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "number", raw: `3`, want: 3},
		{name: "string", raw: `"7"`, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseID(json.RawMessage(tt.raw))
			if err != nil {
				t.Fatalf("parseID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseID() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractString(t *testing.T) {
	raw := json.RawMessage(`{"thread":{"id":"thread-1"},"turn":{"id":"turn-1"}}`)

	got, err := extractString(raw, "thread", "id")
	if err != nil {
		t.Fatalf("extractString() error = %v", err)
	}
	if got != "thread-1" {
		t.Fatalf("extractString() = %q, want thread-1", got)
	}
}
