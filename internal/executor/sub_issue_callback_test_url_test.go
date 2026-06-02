package executor

import (
	"testing"
)

func TestParsePRNumberFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected int
	}{
		{
			name:     "standard github PR url",
			url:      "https://github.com/owner/repo/pull/123",
			expected: 123,
		},
		{
			name:     "github enterprise PR url",
			url:      "https://github.example.com/org/repo/pull/456",
			expected: 456,
		},
		{
			name:     "url with trailing newline",
			url:      "https://github.com/owner/repo/pull/789\n",
			expected: 789, // TrimSpace handles trailing whitespace
		},
		{
			name:     "large PR number",
			url:      "https://github.com/owner/repo/pull/99999",
			expected: 99999,
		},
		{
			name:     "empty string",
			url:      "",
			expected: 0,
		},
		{
			name:     "issue url not PR",
			url:      "https://github.com/owner/repo/issues/123",
			expected: 0,
		},
		{
			name:     "no number after pull",
			url:      "https://github.com/owner/repo/pull/",
			expected: 0,
		},
		{
			name:     "PR url with trailing path",
			url:      "https://github.com/owner/repo/pull/42/files",
			expected: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePRNumberFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("parsePRNumberFromURL(%q) = %d, want %d", tt.url, result, tt.expected)
			}
		})
	}
}
