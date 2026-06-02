package main

import (
	"testing"
)

func TestParseAutopilotBranch(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "valid metadata",
			body: "Some body text\n\n<!-- autopilot-meta branch:pilot/GH-123 -->\n",
			want: "pilot/GH-123",
		},
		{
			name: "metadata with context",
			body: "# Fix\n\n## Context\n- **PR**: #42\n- **Branch**: pilot/GH-99\n\n---\n\n<!-- autopilot-meta branch:pilot/GH-99 -->\n",
			want: "pilot/GH-99",
		},
		{
			name: "no metadata",
			body: "Some body text without metadata",
			want: "",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
		{
			name: "malformed metadata - missing branch",
			body: "<!-- autopilot-meta -->",
			want: "",
		},
		{
			name: "malformed metadata - no closing",
			body: "<!-- autopilot-meta branch:pilot/GH-123",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAutopilotBranch(tt.body)
			if got != tt.want {
				t.Errorf("parseAutopilotBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAutopilotPR(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "valid metadata with pr",
			body: "Some body text\n\n<!-- autopilot-meta branch:pilot/GH-10 pr:42 -->\n",
			want: 42,
		},
		{
			name: "metadata with context",
			body: "# Fix\n\n## Context\n- **PR**: #42\n\n---\n\n<!-- autopilot-meta branch:pilot/GH-99 pr:123 -->\n",
			want: 123,
		},
		{
			name: "missing pr field",
			body: "<!-- autopilot-meta branch:pilot/GH-10 -->",
			want: 0,
		},
		{
			name: "no metadata comment",
			body: "just a normal issue body",
			want: 0,
		},
		{
			name: "empty body",
			body: "",
			want: 0,
		},
		{
			name: "multiple metadata comments - first match wins",
			body: "<!-- autopilot-meta branch:pilot/GH-1 pr:100 -->\nSome text\n<!-- autopilot-meta branch:pilot/GH-2 pr:200 -->",
			want: 100,
		},
		{
			name: "malformed - no closing comment",
			body: "<!-- autopilot-meta branch:pilot/GH-10 pr:42",
			want: 0,
		},
		{
			name: "pr number only",
			body: "<!-- autopilot-meta pr:999 -->",
			want: 999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAutopilotPR(tt.body)
			if got != tt.want {
				t.Errorf("parseAutopilotPR() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseAutopilotIteration(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "valid metadata with iteration",
			body: "Some body\n\n<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:2 -->\n",
			want: 2,
		},
		{
			name: "iteration zero",
			body: "<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:0 -->",
			want: 0,
		},
		{
			name: "no iteration field",
			body: "<!-- autopilot-meta branch:pilot/GH-10 pr:42 -->",
			want: 0,
		},
		{
			name: "no metadata",
			body: "just a normal issue body",
			want: 0,
		},
		{
			name: "empty body",
			body: "",
			want: 0,
		},
		{
			name: "high iteration",
			body: "<!-- autopilot-meta branch:pilot/GH-10 pr:42 iteration:15 -->",
			want: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAutopilotIteration(tt.body)
			if got != tt.want {
				t.Errorf("parseAutopilotIteration() = %d, want %d", got, tt.want)
			}
		})
	}
}
