package intent

import (
	"testing"
)

// TestIsResearch tests research intent detection
func TestIsResearch(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"research this topic", true},
		{"analyze the codebase", true},
		{"review the PR", true},
		{"investigate the bug", true},
		{"summarize the document", true},
		{"compare these options", true},
		{"evaluate the solution", true},
		{"assess the risk", true},
		{"please research this", true},
		{"can you analyze this", true},
		{"i need research on this", true},
		{"hello", false},
		{"create a file", false},
		{"what is this", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsResearch(tt.message)
			if got != tt.expected {
				t.Errorf("IsResearch(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIsPlanning tests planning intent detection
func TestIsPlanning(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"plan how to add auth", true},
		{"design the API", true},
		{"strategy for scaling", true},
		{"how should we handle this", true},
		{"approach for caching", true},
		{"architect the system", true},
		{"outline the feature", true},
		{"hello", false},
		{"create a file", false},
		{"what is this", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsPlanning(tt.message)
			if got != tt.expected {
				t.Errorf("IsPlanning(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIsChat tests chat intent detection
func TestIsChat(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"what do you think about Redis", true},
		{"opinion on microservices", true},
		{"thoughts about this approach", true},
		{"do you recommend Go", true},
		{"should i use TypeScript", true},
		{"is it better to use SQL", true},
		{"discuss the tradeoffs", true},
		{"let's talk about performance", true},
		{"lets talk about caching", true},
		{"hello", false},
		{"create a file", false},
		{"how do I run tests", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsChat(tt.message)
			if got != tt.expected {
				t.Errorf("IsChat(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}
