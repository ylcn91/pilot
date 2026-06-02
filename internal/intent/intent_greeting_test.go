package intent

import (
	"testing"
)

func TestIsGreeting(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"hi", true},
		{"hello", true},
		{"hey", true},
		{"hi there", true},
		{"hello!", true},
		{"hello, how are you", false}, // Too long
		{"hi can you help", false},    // Too long
		{"hiya", false},               // Not exact match
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsGreeting(tt.message)
			if got != tt.expected {
				t.Errorf("IsGreeting(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

func TestStartsWithGreeting(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		// Short greetings (also caught by IsGreeting)
		{"hi", true},
		{"hello", true},
		{"hey", true},

		// Greeting-prefixed longer messages (the main use case)
		{"Hello! How is it going?", true},
		{"hello, how are you today", true},
		{"hey what's up", true},
		{"hi, can you help me?", true},
		{"good morning! how's everything?", true},
		{"good afternoon, quick question", true},
		{"привет, как дела", true},
		{"yo, what's the status?", true},

		// NOT greetings
		{"what is the auth handler?", false},
		{"create a new file", false},
		{"fix the bug", false},
		{"how do I run tests", false},
		{"", false},

		// Too long (> 10 words)
		{"hello I have a very long message that goes on and on about nothing", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := StartsWithGreeting(tt.message)
			if got != tt.expected {
				t.Errorf("StartsWithGreeting(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIsLikelyGreeting tests greeting detection for short messages
func TestIsLikelyGreeting(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"hi", true},
		{"hello", true},
		{"hey", true},
		{"hi there", true},
		{"hello!", true},
		{"hello,", true},
		{"good morning", true},
		{"good afternoon", true},
		{"good evening", true},
		{"howdy", true},
		{"greetings", true},
		{"what's up", true},
		{"whats up", true},
		{"hola", true},
		{"привет", true},
		{"yo", true},
		{"sup", true},
		{"hello how are you today", false}, // too long
		{"hi can you help me with this task", false}, // too long
		{"create file", false},
		{"fix bug", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsLikelyGreeting(tt.message)
			if got != tt.expected {
				t.Errorf("IsLikelyGreeting(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestGreetingPatterns tests that all greeting patterns are recognized
func TestGreetingPatterns(t *testing.T) {
	// Each pattern should be recognized as greeting when alone
	patterns := []string{
		"hi", "hello", "hey", "hola", "привет", "yo", "sup",
		"good morning", "good afternoon", "good evening",
		"howdy", "greetings", "what's up", "whats up",
	}
	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			intent := DetectIntent(pattern)
			if intent != IntentGreeting {
				t.Errorf("DetectIntent(%q) = %v, want %v", pattern, intent, IntentGreeting)
			}
		})
	}
}
