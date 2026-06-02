package executor

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// handleToolUse processes tool usage and updates phase-based progress
func (r *Runner) handleToolUse(taskID, toolName string, input map[string]interface{}, state *progressState) {
	// Log tool usage at debug level
	r.log.Debug("Tool used",
		slog.String("task_id", taskID),
		slog.String("tool", toolName),
	)

	var newPhase string
	var progress int
	var message string

	switch toolName {
	case "Read", "Glob", "Grep":
		state.filesRead++
		if state.phase != "Exploring" {
			newPhase = "Exploring"
			progress = 15
			message = "Analyzing codebase..."
		}

	case "Write", "Edit":
		state.filesWrite++
		if fp, ok := input["file_path"].(string); ok {
			// Track actual modified files with dedup (GH-1388)
			if !strings.Contains(fp, ".agent/") {
				found := false
				for _, existing := range state.modifiedFiles {
					if existing == fp {
						found = true
						break
					}
				}
				if !found {
					state.modifiedFiles = append(state.modifiedFiles, fp)
				}
			}
			// Check if writing to .agent/ (Navigator activity)
			if strings.Contains(fp, ".agent/") {
				state.hasNavigator = true
				if strings.Contains(fp, ".context-markers/") {
					newPhase = "Checkpoint"
					progress = 88
					message = "Creating context marker..."
				} else if strings.Contains(fp, "/tasks/") {
					newPhase = "Documenting"
					progress = 85
					message = "Updating task docs..."
				}
				// Don't report other .agent/ writes
			} else if state.phase != "Implementing" || state.filesWrite == 1 {
				newPhase = "Implementing"
				progress = 40 + min(state.filesWrite*5, 30)
				message = fmt.Sprintf("Creating %s", filepath.Base(fp))
			}
		} else {
			if state.phase != "Implementing" {
				newPhase = "Implementing"
				progress = 40
				message = "Writing files..."
			}
		}

	case "Bash":
		state.commands++
		if cmd, ok := input["command"].(string); ok {
			cmdLower := strings.ToLower(cmd)

			// Detect phase from command (order matters - check specific patterns first)
			if strings.Contains(cmdLower, "git commit") {
				if state.phase != "Committing" {
					newPhase = "Committing"
					progress = 90
					message = "Committing changes..."
				}
			} else if strings.Contains(cmdLower, "git checkout") || strings.Contains(cmdLower, "git branch") {
				if state.phase != "Branching" {
					newPhase = "Branching"
					progress = 10
					message = "Setting up branch..."
				}
			} else if strings.Contains(cmdLower, "pytest") || strings.Contains(cmdLower, "jest") ||
				strings.Contains(cmdLower, "go test") || strings.Contains(cmdLower, "npm test") ||
				strings.Contains(cmdLower, "make test") {
				if state.phase != "Testing" {
					newPhase = "Testing"
					progress = 75
					message = "Running tests..."
				}
			} else if strings.Contains(cmdLower, "npm install") || strings.Contains(cmdLower, "pip install") ||
				strings.Contains(cmdLower, "go mod") {
				if state.phase != "Installing" {
					newPhase = "Installing"
					progress = 30
					message = "Installing dependencies..."
				}
			}
			// Skip other bash commands - too noisy
		}

	case "Task":
		// Sub-agent spawned
		if state.phase != "Delegating" {
			newPhase = "Delegating"
			progress = 50
			if desc, ok := input["description"].(string); ok {
				message = fmt.Sprintf("Spawning agent: %s", truncateText(desc, 40))
			} else {
				message = "Running sub-task..."
			}
		}

	case "Skill":
		// Navigator skill invocation
		if skill, ok := input["skill"].(string); ok {
			state.hasNavigator = true
			skillLower := strings.ToLower(skill)

			switch {
			case strings.HasPrefix(skillLower, "nav-start"):
				newPhase = "Navigator"
				progress = 10
				message = "Starting Navigator session..."
			case strings.HasPrefix(skillLower, "nav-loop"):
				newPhase = "Loop Mode"
				progress = 20
				message = "Entering loop mode..."
			case strings.HasPrefix(skillLower, "nav-task"):
				newPhase = "Task Mode"
				progress = 15
				message = "Task mode activated..."
			case strings.HasPrefix(skillLower, "nav-compact"):
				newPhase = "Compacting"
				progress = 90
				message = "Compacting context..."
			case strings.HasPrefix(skillLower, "nav-marker"):
				newPhase = "Checkpoint"
				progress = 88
				message = "Creating checkpoint..."
			case strings.HasPrefix(skillLower, "nav-simplify"):
				newPhase = "Simplifying"
				progress = 82
				message = "Simplifying code..."
			default:
				// Other nav skills
				if strings.HasPrefix(skillLower, "nav-") {
					message = fmt.Sprintf("Navigator: %s", skill)
				}
			}
		}
	}

	// Only report if phase changed
	if newPhase != "" && newPhase != state.phase {
		state.phase = newPhase
		r.reportProgress(taskID, newPhase, progress, message)
	}
}

// formatToolMessage creates a human-readable message for tool usage
func formatToolMessage(toolName string, input map[string]interface{}) string {
	switch toolName {
	case "Write":
		if fp, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("Writing %s", filepath.Base(fp))
		}
	case "Edit":
		if fp, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("Editing %s", filepath.Base(fp))
		}
	case "Read":
		if fp, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("Reading %s", filepath.Base(fp))
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			return fmt.Sprintf("Running: %s", truncateText(cmd, 40))
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("Searching: %s", pattern)
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("Grep: %s", truncateText(pattern, 30))
		}
	case "Task":
		if desc, ok := input["description"].(string); ok {
			return fmt.Sprintf("Spawning: %s", truncateText(desc, 40))
		}
	}
	return fmt.Sprintf("Using %s", toolName)
}
