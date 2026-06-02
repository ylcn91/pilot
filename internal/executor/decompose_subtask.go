package executor

import (
	"strconv"
	"strings"
)

// analyzeAndSplit breaks a task into subtasks based on structure analysis.
func (d *TaskDecomposer) analyzeAndSplit(task *Task) []*Task {
	desc := task.Description

	// Try different decomposition strategies in order of preference
	var parts []string

	// Strategy 1: Numbered steps (1. 2. 3. or 1) 2) 3))
	parts = extractNumberedSteps(desc)
	if len(parts) >= 2 {
		return d.createSubtasks(task, parts, "step")
	}

	// Strategy 2: Bullet points (- or *)
	parts = extractBulletPoints(desc)
	if len(parts) >= 2 {
		return d.createSubtasks(task, parts, "item")
	}

	// Strategy 3: Acceptance criteria sections
	parts = extractAcceptanceCriteria(desc)
	if len(parts) >= 2 {
		return d.createSubtasks(task, parts, "criteria")
	}

	// Strategy 4: File/module groups mentioned
	parts = extractFileGroups(desc)
	if len(parts) >= 2 {
		return d.createSubtasks(task, parts, "module")
	}

	// No decomposition points found
	return []*Task{task}
}

// createSubtasks generates Task objects from extracted parts.
func (d *TaskDecomposer) createSubtasks(parent *Task, parts []string, partType string) []*Task {
	maxParts := d.config.MaxSubtasks
	if maxParts < 2 {
		maxParts = 2
	}
	if maxParts > 10 {
		maxParts = 10
	}

	if len(parts) > maxParts {
		parts = parts[:maxParts]
	}

	subtasks := make([]*Task, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		subtask := &Task{
			ID:          generateSubtaskID(parent.ID, i+1),
			Title:       truncateTitle(part, 80),
			Description: buildSubtaskDescription(parent, part, i+1, len(parts)),
			ProjectPath: parent.ProjectPath,
			Branch:      parent.Branch,
			BaseBranch:  parent.BaseBranch,
			CreatePR:    false, // Only final subtask creates PR
			Verbose:     parent.Verbose,
		}

		// Last subtask creates the PR
		if i == len(parts)-1 {
			subtask.CreatePR = parent.CreatePR
		}

		subtasks = append(subtasks, subtask)
	}

	return subtasks
}

// generateSubtaskID creates a subtask ID from parent ID.
// Example: "GH-150" -> "GH-150-1", "GH-150-2"
func generateSubtaskID(parentID string, index int) string {
	return parentID + "-" + strconv.Itoa(index)
}

// truncateTitle truncates a string to maxLen, adding ellipsis if needed.
func truncateTitle(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Remove newlines
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// buildSubtaskDescription creates the description for a subtask.
func buildSubtaskDescription(parent *Task, part string, index, total int) string {
	var sb strings.Builder

	sb.WriteString("## Subtask ")
	sb.WriteString(strconv.Itoa(index))
	sb.WriteString(" of ")
	sb.WriteString(strconv.Itoa(total))
	sb.WriteString("\n\n")

	sb.WriteString("**Parent Task:** ")
	sb.WriteString(parent.ID)
	sb.WriteString(" - ")
	sb.WriteString(parent.Title)
	sb.WriteString("\n\n")

	sb.WriteString("## Objective\n\n")
	sb.WriteString(part)
	sb.WriteString("\n\n")

	sb.WriteString("## Context\n\n")
	sb.WriteString("This is part of a larger task that has been decomposed for better execution.\n")
	sb.WriteString("Focus on this specific objective. Other subtasks will handle the remaining work.\n\n")

	if index == total {
		sb.WriteString("**Note:** This is the final subtask. Ensure all previous subtasks are complete before finishing.\n")
	}

	return sb.String()
}

// ShouldDecompose is a convenience function that checks if a task needs decomposition.
// Returns true if the task is complex enough and has sufficient structure for splitting.
// NOTE: Uses heuristic-only detection (DetectComplexity). The full DecomposeWithContext()
// method skips the word count gate when the LLM classifier confirms COMPLEX (GH-1728).
func ShouldDecompose(task *Task, config *DecomposeConfig) bool {
	if config == nil || !config.Enabled {
		return false
	}

	complexity := DetectComplexity(task)
	// Epic tasks always need decomposition
	if complexity == ComplexityEpic {
		return true
	}
	if complexity != ComplexityComplex {
		return false
	}

	wordCount := len(strings.Fields(task.Description))
	return wordCount >= config.MinDescriptionWords
}
