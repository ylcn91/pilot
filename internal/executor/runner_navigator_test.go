package executor

import (
	"testing"
)

func TestNavigatorPatternParsing(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	var lastMessage string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
		lastMessage = message
	})

	tests := []struct {
		name          string
		text          string
		expectedPhase string
		expectedMsg   string
	}{
		{
			name:          "Navigator session started",
			text:          "Navigator Session Started\n━━━━━━━━━",
			expectedPhase: "Navigator",
			expectedMsg:   "Navigator session started",
		},
		{
			name:          "Phase transition IMPL",
			text:          "PHASE: RESEARCH → IMPL\n━━━━━━━━━",
			expectedPhase: "Implement",
			expectedMsg:   "Implementing changes...",
		},
		{
			name:          "Phase transition VERIFY",
			text:          "PHASE: IMPL → VERIFY\n━━━━━━━━━",
			expectedPhase: "Verify",
			expectedMsg:   "Verifying changes...",
		},
		{
			name:          "Loop complete",
			text:          "━━━━━━━━━\nLOOP COMPLETE\n━━━━━━━━━",
			expectedPhase: "Completing",
			expectedMsg:   "Task complete signal received",
		},
		{
			name:          "Exit signal",
			text:          "EXIT_SIGNAL: true",
			expectedPhase: "Finishing",
			expectedMsg:   "Exit signal detected",
		},
		{
			name:          "Task mode complete",
			text:          "TASK MODE COMPLETE\n━━━━━━━━━",
			expectedPhase: "Completing",
			expectedMsg:   "Task complete signal received",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastPhase = ""
			lastMessage = ""
			state := &progressState{phase: "Starting"}

			runner.parseNavigatorPatterns("TASK-1", tt.text, state)

			if lastPhase != tt.expectedPhase {
				t.Errorf("phase = %q, want %q", lastPhase, tt.expectedPhase)
			}
			if lastMessage != tt.expectedMsg {
				t.Errorf("message = %q, want %q", lastMessage, tt.expectedMsg)
			}
		})
	}
}

func TestNavigatorStatusBlockParsing(t *testing.T) {
	runner := NewRunner()
	state := &progressState{phase: "Starting"}

	statusBlock := `NAVIGATOR_STATUS
==================================================
Phase: IMPL
Iteration: 2/5
Progress: 45%

Completion Indicators:
  [x] Code changes committed
==================================================`

	runner.parseNavigatorStatusBlock("TASK-1", statusBlock, state)

	if state.navPhase != "IMPL" {
		t.Errorf("navPhase = %q, want IMPL", state.navPhase)
	}
	if state.navIteration != 2 {
		t.Errorf("navIteration = %d, want 2", state.navIteration)
	}
	if state.navProgress != 45 {
		t.Errorf("navProgress = %d, want 45", state.navProgress)
	}
}

func TestNavigatorSkillDetection(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	tests := []struct {
		skill         string
		expectedPhase string
	}{
		{"nav-start", "Navigator"},
		{"nav-loop", "Loop Mode"},
		{"nav-task", "Task Mode"},
		{"nav-compact", "Compacting"},
		{"nav-marker", "Checkpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.skill, func(t *testing.T) {
			lastPhase = ""
			state := &progressState{phase: "Starting"}

			runner.handleToolUse("TASK-1", "Skill", map[string]interface{}{
				"skill": tt.skill,
			}, state)

			if lastPhase != tt.expectedPhase {
				t.Errorf("phase = %q, want %q", lastPhase, tt.expectedPhase)
			}
			if !state.hasNavigator {
				t.Error("hasNavigator should be true")
			}
		})
	}
}

func TestHandleNavigatorPhaseCases(t *testing.T) {
	tests := []struct {
		phase         string
		expectedPhase string
	}{
		{"INIT", "Init"},
		{"RESEARCH", "Research"},
		{"IMPL", "Implement"},
		{"IMPLEMENTATION", "Implement"},
		{"VERIFY", "Verify"},
		{"VERIFICATION", "Verify"},
		{"COMPLETE", "Complete"},
		{"COMPLETED", "Complete"},
		{"UNKNOWN_PHASE", "UNKNOWN_PHASE"},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			runner := NewRunner()

			var lastPhase string
			runner.OnProgress(func(taskID, phase string, progress int, message string) {
				lastPhase = phase
			})

			state := &progressState{phase: "Starting", navPhase: ""}
			runner.handleNavigatorPhase("TASK-1", tt.phase, state)

			if lastPhase != tt.expectedPhase {
				t.Errorf("phase = %q, want %q", lastPhase, tt.expectedPhase)
			}
		})
	}
}

func TestHandleNavigatorPhaseSkipsSame(t *testing.T) {
	runner := NewRunner()

	var progressCalls int
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		progressCalls++
	})

	state := &progressState{phase: "Starting", navPhase: "IMPL"}

	// Calling with same phase should not trigger progress
	runner.handleNavigatorPhase("TASK-1", "IMPL", state)

	if progressCalls != 0 {
		t.Errorf("progressCalls = %d, want 0 when phase unchanged", progressCalls)
	}
}

func TestParseNavigatorPatternsWorkflowCheck(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}
	runner.parseNavigatorPatterns("TASK-1", "WORKFLOW CHECK - analyzing task", state)

	if lastPhase != "Analyzing" {
		t.Errorf("phase = %q, want Analyzing", lastPhase)
	}
}

func TestParseNavigatorPatternsTaskModeActivated(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}
	runner.parseNavigatorPatterns("TASK-1", "TASK MODE ACTIVATED\n━━━━━━━━━", state)

	if lastPhase != "Task Mode" {
		t.Errorf("phase = %q, want Task Mode", lastPhase)
	}
}

func TestParseNavigatorPatternsStagnation(t *testing.T) {
	runner := NewRunner()

	var lastPhase string
	runner.OnProgress(func(taskID, phase string, progress int, message string) {
		lastPhase = phase
	})

	state := &progressState{phase: "Starting"}
	runner.parseNavigatorPatterns("TASK-1", "STAGNATION DETECTED - retrying", state)

	if lastPhase != "⚠️ Stalled" {
		t.Errorf("phase = %q, want ⚠️ Stalled", lastPhase)
	}
}
