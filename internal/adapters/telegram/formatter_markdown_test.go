package telegram

import (
	"strings"
	"testing"
)

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"hello_world", "\\_"},
		{"*bold*", "\\*"},
		{"[link]", "\\["},
		{"plain text", "plain text"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeMarkdown(tt.input)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("escapeMarkdown(%q) = %q, want to contain %q", tt.input, got, tt.contains)
			}
		})
	}
}

// TestEscapeMarkdownAllChars tests all markdown special characters
func TestEscapeMarkdownAllChars(t *testing.T) {
	specialChars := []struct {
		char    string
		escaped string
	}{
		{"_", "\\_"},
		{"*", "\\*"},
		{"[", "\\["},
		{"]", "\\]"},
		{"(", "\\("},
		{")", "\\)"},
		{"~", "\\~"},
		{">", "\\>"},
		{"#", "\\#"},
		{"+", "\\+"},
		{"-", "\\-"},
		{"=", "\\="},
		{"|", "\\|"},
		{"{", "\\{"},
		{"}", "\\}"},
		{".", "\\."},
		{"!", "\\!"},
	}

	for _, tc := range specialChars {
		t.Run("char_"+tc.char, func(t *testing.T) {
			input := "text" + tc.char + "more"
			got := escapeMarkdown(input)
			if !strings.Contains(got, tc.escaped) {
				t.Errorf("escapeMarkdown(%q) = %q, want to contain %q", input, got, tc.escaped)
			}
		})
	}
}

func TestConvertTablesToLists(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name: "simple table",
			input: `Here's a table:
| Task | Status |
|------|--------|
| TASK-01 | Done |
| TASK-02 | Pending |`,
			contains: []string{"• TASK-01: Done", "• TASK-02: Pending"},
			excludes: []string{"|---"},
		},
		{
			name: "table with description",
			input: `| Name | Description | Priority |
|------|-------------|----------|
| Fix bug | Critical issue | High |`,
			contains: []string{"• Fix bug: Critical issue | High"},
		},
		{
			name:     "no table",
			input:    "Just regular text\nNo tables here",
			contains: []string{"Just regular text", "No tables here"},
		},
		{
			name: "mixed content",
			input: `## Summary
Some text before.

| Item | Value |
|------|-------|
| A | 1 |

Some text after.`,
			contains: []string{"## Summary", "• A: 1", "Some text after"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTablesToLists(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("convertTablesToLists() =\n%s\nwant to contain %q", got, want)
				}
			}
			for _, exclude := range tt.excludes {
				if strings.Contains(got, exclude) {
					t.Errorf("convertTablesToLists() =\n%s\nshould NOT contain %q", got, exclude)
				}
			}
		})
	}
}

// TestConvertTablesToListsComplex tests complex table scenarios
func TestConvertTablesToListsComplex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name: "table with many columns",
			input: `| Name | Status | Priority | Owner |
|------|--------|----------|-------|
| Task1 | Done | High | Alice |
| Task2 | WIP | Low | Bob |`,
			contains: []string{"Task1: Done", "Task2: WIP"},
			excludes: []string{"|---"},
		},
		{
			name: "multiple tables",
			input: `First table:
| A | B |
|---|---|
| 1 | 2 |

Second table:
| C | D |
|---|---|
| 3 | 4 |`,
			contains: []string{"1", "2", "3", "4"},
		},
		{
			name: "text between tables",
			input: `Before
| X | Y |
|---|---|
| a | b |
Middle text
| P | Q |
|---|---|
| c | d |
After`,
			contains: []string{"Before", "Middle text", "After", "a", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTablesToLists(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("convertTablesToLists() should contain %q, got:\n%s", want, got)
				}
			}
			for _, exclude := range tt.excludes {
				if strings.Contains(got, exclude) {
					t.Errorf("convertTablesToLists() should NOT contain %q, got:\n%s", exclude, got)
				}
			}
		})
	}
}

// TestParseTableRow tests table row parsing
func TestParseTableRow(t *testing.T) {
	tests := []struct {
		row      string
		expected []string
	}{
		{
			row:      "| Col1 | Col2 | Col3 |",
			expected: []string{"Col1", "Col2", "Col3"},
		},
		{
			row:      "|A|B|",
			expected: []string{"A", "B"},
		},
		{
			row:      "| Single |",
			expected: []string{"Single"},
		},
		{
			row:      "| --- | --- |",
			expected: []string{},
		},
		{
			row:      "|  Spaces  |  Here  |",
			expected: []string{"Spaces", "Here"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.row, func(t *testing.T) {
			got := parseTableRow(tt.row)
			if len(got) != len(tt.expected) {
				t.Errorf("parseTableRow() len = %d, want %d", len(got), len(tt.expected))
				return
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("parseTableRow()[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

// TestMin tests the min helper function
func TestMin(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-1, 1, -1},
		{100, 50, 50},
	}

	for _, tt := range tests {
		got := min(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "needs truncation",
			input:    "hello world this is a long string",
			maxLen:   15,
			expected: "hello world ...",
		},
		{
			name:     "removes newlines",
			input:    "hello\nworld",
			maxLen:   20,
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDescription(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncateDescription() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestTruncateDescriptionEdgeCases tests edge cases
func TestTruncateDescriptionEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   \n\t  ",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "exactly max length",
			input:    "12345",
			maxLen:   5,
			expected: "12345",
		},
		{
			name:     "one char over",
			input:    "123456",
			maxLen:   5,
			expected: "12...",
		},
		{
			name:     "multiple newlines",
			input:    "line1\nline2\nline3",
			maxLen:   50,
			expected: "line1 line2 line3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDescription(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncateDescription(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}
