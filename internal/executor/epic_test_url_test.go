package executor

import (
	"testing"
)

func TestParsePRNumber(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected int
	}{
		{"standard PR URL", "https://github.com/owner/repo/pull/42", 42},
		{"pulls variant (not matched)", "https://github.com/owner/repo/pulls/99", 0},
		{"enterprise URL", "https://github.example.com/org/repo/pull/7", 7},
		{"large PR number", "https://github.com/owner/repo/pull/12345", 12345},
		{"empty string", "", 0},
		{"no pull path", "https://github.com/owner/repo/issues/123", 0},
		{"plain text", "not a url", 0},
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

func TestSetOnSubIssuePRCreated(t *testing.T) {
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	// Callback should be nil by default
	if runner.onSubIssuePRCreated != nil {
		t.Error("onSubIssuePRCreated should be nil by default")
	}

	// Set callback
	var called bool
	var capturedPR int
	var capturedURL string
	var capturedIssue int
	var capturedSHA string
	var capturedBranch string

	runner.SetOnSubIssuePRCreated(func(prNumber int, prURL string, issueNumber int, headSHA string, branchName string, issueNodeID string) {
		called = true
		capturedPR = prNumber
		capturedURL = prURL
		capturedIssue = issueNumber
		capturedSHA = headSHA
		capturedBranch = branchName
	})

	if runner.onSubIssuePRCreated == nil {
		t.Fatal("onSubIssuePRCreated should be set after SetOnSubIssuePRCreated")
	}

	// Invoke callback
	runner.onSubIssuePRCreated(42, "https://github.com/owner/repo/pull/42", 10, "abc123", "pilot/GH-10", "")

	if !called {
		t.Error("callback was not invoked")
	}
	if capturedPR != 42 {
		t.Errorf("prNumber = %d, want 42", capturedPR)
	}
	if capturedURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("prURL = %q, want pull/42 URL", capturedURL)
	}
	if capturedIssue != 10 {
		t.Errorf("issueNumber = %d, want 10", capturedIssue)
	}
	if capturedSHA != "abc123" {
		t.Errorf("headSHA = %q, want abc123", capturedSHA)
	}
	if capturedBranch != "pilot/GH-10" {
		t.Errorf("branchName = %q, want pilot/GH-10", capturedBranch)
	}
}

func TestParseIssueNumber(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected int
	}{
		{
			name:     "standard github issue url",
			url:      "https://github.com/ylcn91/pilot/issues/123",
			expected: 123,
		},
		{
			name:     "github enterprise url",
			url:      "https://github.example.com/org/repo/issues/456",
			expected: 456,
		},
		{
			name:     "url with trailing newline",
			url:      "https://github.com/owner/repo/issues/789\n",
			expected: 789,
		},
		{
			name:     "large issue number",
			url:      "https://github.com/owner/repo/issues/99999",
			expected: 99999,
		},
		{
			name:     "empty string",
			url:      "",
			expected: 0,
		},
		{
			name:     "invalid url - no issues path",
			url:      "https://github.com/owner/repo/pull/123",
			expected: 0,
		},
		{
			name:     "invalid url - no number",
			url:      "https://github.com/owner/repo/issues/",
			expected: 0,
		},
		{
			name:     "plain text",
			url:      "not a url at all",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseIssueNumber(tt.url)
			if result != tt.expected {
				t.Errorf("parseIssueNumber(%q) = %d, want %d", tt.url, result, tt.expected)
			}
		})
	}
}
