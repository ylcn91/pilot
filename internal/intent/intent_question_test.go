package intent

import (
	"testing"
)

func TestIsQuestion(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"what is the structure?", true},
		{"how do I run this", true},
		{"where is config", true},
		{"explain the auth", true},
		{"show me the files", true},
		{"create a file", false}, // Task word takes precedence
		{"fix the bug", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsQuestion(tt.message)
			if got != tt.expected {
				t.Errorf("IsQuestion(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestQuestionPatterns tests that question patterns work
func TestQuestionPatterns(t *testing.T) {
	// Each pattern should trigger question detection
	testMessages := []string{
		"what is the project structure?",
		"how do I run the tests?",
		"where is the config file?",
		"why is this failing?",
		"when is the release?",
		"who is the maintainer?",
		"can you tell me about auth?",
		"do you know how this works?",
		"is there a test file?",
	}

	for _, msg := range testMessages {
		t.Run(msg, func(t *testing.T) {
			intent := DetectIntent(msg)
			if intent != IntentQuestion {
				t.Errorf("DetectIntent(%q) = %v, want %v", msg, intent, IntentQuestion)
			}
		})
	}
}

// TestQuestionKeywordsWithoutActions tests question keywords
func TestQuestionKeywordsWithoutActions(t *testing.T) {
	tests := []struct {
		message  string
		expected Intent
	}{
		{"what are the issues", IntentQuestion},
		{"show me the tasks", IntentQuestion},
		{"list the backlog", IntentQuestion},
		{"check the status", IntentQuestion},
		{"show todos", IntentQuestion},
		{"what are the fixmes", IntentQuestion},
		{"tell me about the project", IntentQuestion},
		{"describe the architecture", IntentQuestion},
		{"find all handlers", IntentQuestion},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := DetectIntent(tt.message)
			if got != tt.expected {
				t.Errorf("DetectIntent(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIsClearQuestion tests the IsClearQuestion function for LLM pre-check (GH-382)
func TestIsClearQuestion(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Should be clear questions - ends with ?
		{"What's in roadmap?", true},
		{"What's in the backlog?", true},
		{"How does auth work?", true},
		{"Where is the config file?", true},
		{"Why is this failing?", true},
		{"Can you explain the architecture?", true},

		// Should be clear questions - question starters (no ?)
		{"What's in roadmap", true},
		{"What is in the backlog", true},
		{"How does the auth system work", true},
		{"How do I run tests", true},
		{"How can I debug this", true},
		{"Where is the config", true},
		{"Where are the tests", true},
		{"Why is this failing", true},
		{"Why are we using Go", true},
		{"Why does it crash", true},
		{"When is the release", true},
		{"When does it deploy", true},
		{"When will it be ready", true},
		{"Who is the maintainer", true},
		{"Who are the contributors", true},
		{"Which library should I use", true},
		{"Can you explain the flow", true},
		{"Could you explain the architecture", true},

		// Should NOT be clear questions (need LLM)
		{"Add a logout button", false},
		{"Fix the auth bug", false},
		{"What do you think about adding X", false}, // chat, not question
		{"Create a new endpoint", false},
		{"Implement feature X", false},
		{"Hello", false},
		{"Hi there", false},
		{"Research authentication methods", false},
		{"Plan the migration", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsClearQuestion(tt.input)
			if got != tt.expected {
				t.Errorf("IsClearQuestion(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
