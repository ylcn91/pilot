package telegram

import (
	"strings"
	"testing"
)

func TestFormatGreeting(t *testing.T) {
	tests := []struct {
		name     string
		username string
		contains []string
	}{
		{
			name:     "with username",
			username: "Alice",
			contains: []string{"👋", "Alice"},
		},
		{
			name:     "without username",
			username: "",
			contains: []string{"👋", "there"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatGreeting(tt.username)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatGreeting() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestFormatTaskConfirmation(t *testing.T) {
	got := FormatTaskConfirmation("TG-123", "Add auth handler", "/project/path")

	wants := []string{"📋", "TG-123", "auth handler", "/project/path"}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("FormatTaskConfirmation() = %q, want to contain %q", got, want)
		}
	}
}

func TestFormatProgressUpdate(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		phase    string
		progress int
		message  string
		contains []string
	}{
		{
			name:     "starting phase",
			taskID:   "TG-123",
			phase:    "Starting",
			progress: 0,
			message:  "Initializing...",
			contains: []string{"🚀", "Starting", "(0%)", "TG-123", "░░░░░░░░░░░░░░░░░░░░", "Initializing"},
		},
		{
			name:     "exploring phase",
			taskID:   "TG-456",
			phase:    "Exploring",
			progress: 25,
			message:  "Reading files",
			contains: []string{"🔍", "Exploring", "25%", "█████░░░░░░░░░░░░░░░"},
		},
		{
			name:     "implementing phase 50%",
			taskID:   "TG-789",
			phase:    "Implementing",
			progress: 50,
			message:  "Creating handler.go",
			contains: []string{"⚙️", "Implementing", "50%", "██████████░░░░░░░░░░", "handler"}, // .go gets escaped to \.go
		},
		{
			name:     "testing phase",
			taskID:   "TG-999",
			phase:    "Testing",
			progress: 75,
			message:  "Running tests...",
			contains: []string{"🧪", "Testing", "75%", "███████████████░░░░░"},
		},
		{
			name:     "committing phase",
			taskID:   "TG-111",
			phase:    "Committing",
			progress: 90,
			message:  "",
			contains: []string{"💾", "Committing", "90%", "██████████████████░░"},
		},
		{
			name:     "completed phase",
			taskID:   "TG-222",
			phase:    "Completed",
			progress: 100,
			message:  "",
			contains: []string{"✅", "Completed", "100%", "████████████████████"},
		},
		{
			name:     "navigator phase",
			taskID:   "TG-333",
			phase:    "Navigator",
			progress: 10,
			message:  "Loading session",
			contains: []string{"🧭", "Navigator", "10%"},
		},
		{
			name:     "unknown phase uses default emoji",
			taskID:   "TG-444",
			phase:    "CustomPhase",
			progress: 40,
			message:  "",
			contains: []string{"⏳", "CustomPhase", "40%"},
		},
		{
			name:     "progress clamped to max 100",
			taskID:   "TG-555",
			phase:    "Completed",
			progress: 150,
			message:  "",
			contains: []string{"████████████████████"}, // Full bar
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatProgressUpdate(tt.taskID, tt.phase, tt.progress, tt.message)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatProgressUpdate() =\n%s\nwant to contain %q", got, want)
				}
			}
		})
	}
}

// TestFormatProgressUpdateBranchingPhase tests branching phase emoji
func TestFormatProgressUpdateBranchingPhase(t *testing.T) {
	got := FormatProgressUpdate("TG-100", "Branching", 5, "Creating branch")
	if !strings.Contains(got, "🌿") {
		t.Errorf("FormatProgressUpdate() should contain branching emoji, got:\n%s", got)
	}
}

// TestFormatProgressUpdateInstallingPhase tests installing phase emoji
func TestFormatProgressUpdateInstallingPhase(t *testing.T) {
	got := FormatProgressUpdate("TG-100", "Installing", 20, "npm install")
	if !strings.Contains(got, "📦") {
		t.Errorf("FormatProgressUpdate() should contain installing emoji, got:\n%s", got)
	}
}

// TestFormatProgressUpdateNegativeProgress tests negative progress clamping
func TestFormatProgressUpdateNegativeProgress(t *testing.T) {
	got := FormatProgressUpdate("TG-100", "Starting", -10, "")
	// Should have empty progress bar (all ░)
	if !strings.Contains(got, "░░░░░░░░░░░░░░░░░░░░") {
		t.Errorf("FormatProgressUpdate() should have empty bar for negative progress, got:\n%s", got)
	}
}

// TestFormatTaskStarted tests task started message formatting
func TestFormatTaskStarted(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		description string
		contains    []string
	}{
		{
			name:        "basic task",
			taskID:      "TASK-01",
			description: "Create auth handler",
			contains:    []string{"🚀", "Executing", "TASK-01", "auth handler"},
		},
		{
			name:        "long description truncated",
			taskID:      "TG-123",
			description: strings.Repeat("a", 200),
			contains:    []string{"TG-123", "..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTaskStarted(tt.taskID, tt.description)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatTaskStarted() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

// TestFormatQuestionAck tests question acknowledgment
func TestFormatQuestionAck(t *testing.T) {
	got := FormatQuestionAck()
	if !strings.Contains(got, "🔍") {
		t.Errorf("FormatQuestionAck() should contain search emoji, got: %q", got)
	}
	if !strings.Contains(got, "Looking") {
		t.Errorf("FormatQuestionAck() should contain 'Looking', got: %q", got)
	}
}

// TestFormatQuestionAnswer tests question answer formatting
func TestFormatQuestionAnswer(t *testing.T) {
	tests := []struct {
		name     string
		answer   string
		contains []string
		excludes []string
	}{
		{
			name:     "simple answer",
			answer:   "The auth module handles user authentication.",
			contains: []string{"auth module", "authentication"},
		},
		{
			name:     "answer with internal signals cleaned",
			answer:   "Answer here\nEXIT_SIGNAL: true\nMore text",
			contains: []string{"Answer here", "More text"},
			excludes: []string{"EXIT_SIGNAL"},
		},
		{
			name:     "answer with table converted",
			answer:   "Info:\n| Col1 | Col2 |\n|---|---|\n| A | B |",
			contains: []string{"A", "B"},
			excludes: []string{"|---|"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatQuestionAnswer(tt.answer)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatQuestionAnswer() = %q, want to contain %q", got, want)
				}
			}
			for _, exclude := range tt.excludes {
				if strings.Contains(got, exclude) {
					t.Errorf("FormatQuestionAnswer() = %q, should NOT contain %q", got, exclude)
				}
			}
		})
	}
}

// TestFormatQuestionAnswerTruncation tests long answer truncation
func TestFormatQuestionAnswerTruncation(t *testing.T) {
	longAnswer := strings.Repeat("a", 4000)
	got := FormatQuestionAnswer(longAnswer)

	if len(got) > 3600 { // 3500 + buffer for truncation message
		t.Errorf("FormatQuestionAnswer() length = %d, want <= 3600", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("FormatQuestionAnswer() should contain truncation indicator")
	}
}
