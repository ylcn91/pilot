package intent

import (
	"testing"
)

func TestContainsTaskReference(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"work on task-04", true},
		{"pick 04", true},
		{"do #123", true},
		{"TASK-123", true},
		{"hello", false},
		{"what is this", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := ContainsTaskReference(tt.message)
			if got != tt.expected {
				t.Errorf("ContainsTaskReference(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestIsTask tests the IsTask function
func TestIsTask(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		{"create a new file", true},
		{"add authentication", true},
		{"fix the bug", true},
		{"update the readme", true},
		{"implement feature x", true},
		{"refactor the code", true},
		{"delete old files", true},
		{"remove unused imports", true},
		{"please create a file", true},
		{"can you add a test", true},
		{"i need fix for this", true}, // "i need <action>" pattern
		{"i want update docs", true},  // "i want <action>" pattern
		{"hello world", false},
		{"what is this", false},
		{"show me the code", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsTask(tt.message)
			if got != tt.expected {
				t.Errorf("IsTask(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestContainsActionWord tests action word detection
func TestContainsActionWord(t *testing.T) {
	tests := []struct {
		message  string
		expected bool
	}{
		// Starts with action
		{"create file", true},
		{"add test", true},
		{"fix bug", true},
		{"update docs", true},
		{"implement feature", true},
		{"refactor code", true},
		{"delete file", true},
		{"remove line", true},
		{"generate report", true},
		{"setup project", true},
		{"configure settings", true},
		{"install package", true},
		{"write test", true},
		{"build project", true},
		{"make changes", true},
		{"modify file", true},
		{"change config", true},
		{"edit code", true},
		// Meta-task actions
		// Note: "review" moved to research patterns (GH-290)
		{"prioritize backlog", true},
		{"reorder items", true},
		{"sort list", true},
		{"organize files", true},
		{"rank tasks", true},
		{"triage issues", true},
		{"set priority high", true},
		// With prefixes
		{"please create a file", true},
		{"can you add a test", true},
		{"i need fix for this", true},    // "i need <action>" pattern (no "to" between)
		{"i want update the docs", true}, // "i want <action>" pattern (no "to" between)
		// Non-action messages
		{"hello", false},
		{"what is this", false},
		{"show me", false},
		{"explain how", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := ContainsActionWord(tt.message)
			if got != tt.expected {
				t.Errorf("ContainsActionWord(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

// TestTaskActionWords tests task action word patterns
func TestTaskActionWords(t *testing.T) {
	// All action words should be recognized
	// Note: "review" excluded as it's now a research pattern (see TestIsResearch)
	actions := []string{
		"create", "add", "make", "build", "implement",
		"fix", "update", "modify", "change", "edit",
		"delete", "remove", "refactor", "write",
		"generate", "setup", "configure", "install",
		"prioritize", "reprioritize", "reorder",
		"sort", "organize", "rank", "triage",
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			msg := action + " something"
			intent := DetectIntent(msg)
			if intent != IntentTask {
				t.Errorf("DetectIntent(%q) = %v, want %v", msg, intent, IntentTask)
			}
		})
	}
}
