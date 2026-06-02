package executor

import (
	"strconv"
	"strings"
	"testing"
)

func TestDecomposeConfig_Defaults(t *testing.T) {
	config := DefaultDecomposeConfig()

	if config.Enabled {
		t.Error("Expected Enabled to be false by default")
	}
	if config.MinComplexity != "complex" {
		t.Errorf("Expected MinComplexity to be 'complex', got %q", config.MinComplexity)
	}
	if config.MaxSubtasks != 5 {
		t.Errorf("Expected MaxSubtasks to be 5, got %d", config.MaxSubtasks)
	}
	if config.MinDescriptionWords != 50 {
		t.Errorf("Expected MinDescriptionWords to be 50, got %d", config.MinDescriptionWords)
	}
}

func TestTaskDecomposer_Disabled(t *testing.T) {
	config := &DecomposeConfig{
		Enabled: false,
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:          "TEST-1",
		Title:       "Complex Task",
		Description: "This is a very complex task with many steps that should be decomposed but won't be because decomposition is disabled.",
	}

	result := decomposer.Decompose(task)

	if result.Decomposed {
		t.Error("Expected Decomposed to be false when disabled")
	}
	if len(result.Subtasks) != 1 {
		t.Errorf("Expected 1 subtask (original), got %d", len(result.Subtasks))
	}
	if result.Reason != "decomposition disabled" {
		t.Errorf("Expected reason 'decomposition disabled', got %q", result.Reason)
	}
}

func TestTaskDecomposer_NilTask(t *testing.T) {
	decomposer := NewTaskDecomposer(&DecomposeConfig{Enabled: true})

	result := decomposer.Decompose(nil)

	if result.Decomposed {
		t.Error("Expected Decomposed to be false for nil task")
	}
	if result.Subtasks != nil {
		t.Error("Expected nil subtasks for nil task")
	}
	if result.Reason != "nil task" {
		t.Errorf("Expected reason 'nil task', got %q", result.Reason)
	}
}

func TestTaskDecomposer_SimpleTasks(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 50,
	}
	decomposer := NewTaskDecomposer(config)

	// Simple task - should not decompose
	task := &Task{
		ID:          "TEST-1",
		Title:       "Fix typo",
		Description: "Fix a typo in the README file",
	}

	result := decomposer.Decompose(task)

	if result.Decomposed {
		t.Error("Expected simple task to not be decomposed")
	}
	if !strings.Contains(result.Reason, "complexity below threshold") {
		t.Errorf("Expected reason about complexity, got %q", result.Reason)
	}
}

func TestTaskDecomposer_NumberedSteps(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10, // Lower for testing
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "TEST-1",
		Title: "Refactor authentication system",
		Description: `This task requires refactoring the entire authentication system with multiple changes:

1. Update the user model to include new fields for MFA
2. Refactor the login endpoint to support MFA flow
3. Add new middleware for session validation
4. Update the frontend components for MFA input
5. Add comprehensive tests for all changes

This is a complex architectural change that spans multiple files.`,
		ProjectPath: "/test/project",
		Branch:      "feature/auth-refactor",
		CreatePR:    true,
	}

	result := decomposer.Decompose(task)

	if !result.Decomposed {
		t.Errorf("Expected task to be decomposed, reason: %s", result.Reason)
		return
	}

	if len(result.Subtasks) != 5 {
		t.Errorf("Expected 5 subtasks, got %d", len(result.Subtasks))
	}

	// Verify subtask IDs
	for i, subtask := range result.Subtasks {
		expectedID := "TEST-1-" + strconv.Itoa(i+1)
		if subtask.ID != expectedID {
			t.Errorf("Subtask %d: expected ID %q, got %q", i, expectedID, subtask.ID)
		}

		// Verify project path propagation
		if subtask.ProjectPath != task.ProjectPath {
			t.Errorf("Subtask %d: ProjectPath not propagated", i)
		}

		// Verify branch propagation
		if subtask.Branch != task.Branch {
			t.Errorf("Subtask %d: Branch not propagated", i)
		}

		// Only last subtask should create PR
		if i < len(result.Subtasks)-1 && subtask.CreatePR {
			t.Errorf("Subtask %d: should not create PR", i)
		}
		if i == len(result.Subtasks)-1 && !subtask.CreatePR {
			t.Error("Last subtask should create PR")
		}
	}
}

func TestTaskDecomposer_BulletPoints(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "TEST-2",
		Title: "Database migration for new schema",
		Description: `Migrate the database to support the new multi-tenant architecture:

- Create tenant table with proper indexes
- Add tenant_id column to all user-facing tables
- Implement row-level security policies
- Update all queries to filter by tenant

This migration is critical for the multi-tenant feature.`,
		ProjectPath: "/test/project",
	}

	result := decomposer.Decompose(task)

	if !result.Decomposed {
		t.Errorf("Expected task to be decomposed, reason: %s", result.Reason)
		return
	}

	if len(result.Subtasks) != 4 {
		t.Errorf("Expected 4 subtasks, got %d", len(result.Subtasks))
	}
}

func TestTaskDecomposer_AcceptanceCriteria(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "TEST-3",
		Title: "Rewrite the API layer",
		Description: `Rewrite the API layer to use the new framework with the following criteria:

## Acceptance Criteria
- [ ] All existing endpoints migrated to new router
- [ ] OpenAPI spec generated automatically
- [ ] Request validation using new middleware
- [ ] Response serialization updated

This is a major rewrite that requires careful planning.`,
		ProjectPath: "/test/project",
	}

	result := decomposer.Decompose(task)

	if !result.Decomposed {
		t.Errorf("Expected task to be decomposed, reason: %s", result.Reason)
		return
	}

	if len(result.Subtasks) != 4 {
		t.Errorf("Expected 4 subtasks, got %d", len(result.Subtasks))
	}
}

func TestTaskDecomposer_MaxSubtasks(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         3, // Limit to 3
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "TEST-4",
		Title: "Restructure the entire codebase",
		Description: `Major restructuring with many steps:

1. Step one of the restructure
2. Step two of the restructure
3. Step three of the restructure
4. Step four of the restructure
5. Step five of the restructure
6. Step six of the restructure

This is extensive work requiring many changes.`,
		ProjectPath: "/test/project",
	}

	result := decomposer.Decompose(task)

	if !result.Decomposed {
		t.Errorf("Expected task to be decomposed, reason: %s", result.Reason)
		return
	}

	if len(result.Subtasks) != 3 {
		t.Errorf("Expected max 3 subtasks, got %d", len(result.Subtasks))
	}
}

func TestTaskDecomposer_ShortDescription(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 100, // High threshold
	}
	decomposer := NewTaskDecomposer(config)

	task := &Task{
		ID:    "TEST-5",
		Title: "Refactor something",
		Description: `Short refactor task:
1. Do this
2. Do that`,
		ProjectPath: "/test/project",
	}

	result := decomposer.Decompose(task)

	if result.Decomposed {
		t.Error("Expected short description to not be decomposed")
	}
	if result.Reason != "description too short for decomposition (heuristic mode)" {
		t.Errorf("Expected reason about short description, got %q", result.Reason)
	}
}

func TestTaskDecomposer_NoDecompositionPoints(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 10,
	}
	decomposer := NewTaskDecomposer(config)

	// Complex task but no clear structure for decomposition
	task := &Task{
		ID:    "TEST-6",
		Title: "Refactor the system architecture completely",
		Description: `This is a complex refactoring task that involves updating the system
architecture to support new requirements. The changes will touch multiple files
and modules across the codebase. We need to ensure backward compatibility while
improving the overall design. This is quite complex and requires careful consideration
of all the components involved in the system and how they interact with each other.`,
		ProjectPath: "/test/project",
	}

	result := decomposer.Decompose(task)

	if result.Decomposed {
		t.Error("Expected task without clear structure to not decompose")
	}
	if result.Reason != "no decomposition points found" {
		t.Errorf("Expected reason 'no decomposition points found', got %q", result.Reason)
	}
}
