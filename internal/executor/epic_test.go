package executor

import (
	"reflect"
	"strings"
	"testing"
)

// TestAllChildrenDone covers GH-3053: empty slice must NOT vacuously satisfy
// "all done". The original implementation returned true for the empty case,
// causing recoverExistingSubIssues failures (network blip, gh search hiccup,
// no sub-issues yet) to be silently interpreted as "epic complete" — leading
// to the false 100% completion log + exit-without-work on the parent issue.
func TestAllChildrenDone(t *testing.T) {
	tests := []struct {
		name   string
		issues []CreatedIssue
		want   bool
	}{
		{"empty slice: not done", nil, false},
		{"empty slice literal: not done", []CreatedIssue{}, false},
		{"single open: not done", []CreatedIssue{{State: "open"}}, false},
		{"single closed: done", []CreatedIssue{{State: "closed"}}, true},
		{"mixed open+closed: not done", []CreatedIssue{{State: "closed"}, {State: "open"}}, false},
		{"all closed: done", []CreatedIssue{{State: "closed"}, {State: "CLOSED"}}, true},
		{"case-insensitive open detection", []CreatedIssue{{State: "OPEN"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allChildrenDone(tt.issues); got != tt.want {
				t.Errorf("allChildrenDone(%v) = %v, want %v", tt.issues, got, tt.want)
			}
		})
	}
}

func TestBuildPlanningPrompt(t *testing.T) {
	task := &Task{
		ID:          "TASK-123",
		Title:       "Implement user authentication",
		Description: "Add login, logout, and session management",
		Labels:      []string{"pilot", "area:auth"},
	}

	// Empty agentDir skips project-context/SOP priming so the test stays
	// independent of any on-disk .agent/ directory.
	prompt := buildPlanningPrompt(task, "")

	// Check required elements are present
	required := []string{
		"software architect",
		"3-5 sequential subtasks",
		"Implement user authentication",
		"Add login, logout, and session management",
		"Output Format",
		"Single-Package Splits",         // GH-1265: anti-cascade instruction
		"NEVER split work that belongs", // GH-1265: footer reminder
		"Do NOT propose abstractions",   // anti-over-engineering instruction
		"Labels: pilot, area:auth",      // labels hint line
	}

	for _, r := range required {
		if !strings.Contains(prompt, r) {
			t.Errorf("prompt missing required element: %q", r)
		}
	}

	// GH-2559: Prompt examples must NOT include concrete strings the LLM can mistake
	// for real subtasks. On 2026-05-03 the planner copied
	// "feat(auth): add OAuth provider integration" verbatim from the prompt and
	// Pilot improvised an OAuth implementation, triggering a 17-hour cascade
	// across 12 providers (reverted in PR #2558). Examples must be ALL_CAPS
	// placeholder slots, never plausible-sounding tasks.
	forbidden := []string{
		"feat(auth): add OAuth provider integration",
		"fix(api): handle nil response in webhook handler",
		"chore(deps): upgrade go modules to latest",
	}
	for _, f := range forbidden {
		if strings.Contains(prompt, f) {
			t.Errorf("prompt contains forbidden concrete example %q — must be a placeholder template (GH-2559)", f)
		}
	}
}

func TestConsolidateEpicPlan(t *testing.T) {
	subtasks := []PlannedSubtask{
		{Title: "First step", Description: "Do the first thing", Order: 1},
		{Title: "Second step", Description: "Do the second thing", Order: 2},
		{Title: "Third step", Description: "", Order: 3},
	}

	result := consolidateEpicPlan("Original description", subtasks)

	if !strings.Contains(result, "Original description") {
		t.Error("should contain original description")
	}
	if !strings.Contains(result, "Planned Steps") {
		t.Error("should contain Planned Steps header")
	}
	if !strings.Contains(result, "1. **First step** — Do the first thing") {
		t.Error("should contain first subtask with description")
	}
	if !strings.Contains(result, "2. **Second step** — Do the second thing") {
		t.Error("should contain second subtask with description")
	}
	if !strings.Contains(result, "3. **Third step**") {
		t.Error("should contain third subtask")
	}
	// Third subtask has no description, so no " — " separator
	if strings.Contains(result, "3. **Third step** —") {
		t.Error("third subtask should not have separator when description is empty")
	}
}

func TestEpicPlanTypes(t *testing.T) {
	// Verify types are properly constructed
	task := &Task{ID: "TASK-1", Title: "Epic task"}
	subtasks := []PlannedSubtask{
		{Title: "First", Description: "Do first thing", Order: 1},
		{Title: "Second", Description: "Do second thing", Order: 2, DependsOn: []int{1}},
	}

	plan := &EpicPlan{
		ParentTask:  task,
		Subtasks:    subtasks,
		TotalEffort: "2 days",
		PlanOutput:  "raw output",
	}

	if plan.ParentTask.ID != "TASK-1" {
		t.Errorf("ParentTask.ID = %q, want %q", plan.ParentTask.ID, "TASK-1")
	}
	if len(plan.Subtasks) != 2 {
		t.Errorf("len(Subtasks) = %d, want 2", len(plan.Subtasks))
	}
	if !reflect.DeepEqual(plan.Subtasks[1].DependsOn, []int{1}) {
		t.Errorf("Subtasks[1].DependsOn = %v, want [1]", plan.Subtasks[1].DependsOn)
	}
}

func TestExecuteEpicTriggersPlanningMode(t *testing.T) {
	// Test that epic complexity triggers planning mode
	task := &Task{
		ID:          "TASK-EPIC",
		Title:       "[epic] Major refactoring",
		Description: "This is a large epic task with multiple phases",
	}

	complexity := DetectComplexity(task)
	if !complexity.IsEpic() {
		t.Error("expected epic complexity to be detected")
	}
}
