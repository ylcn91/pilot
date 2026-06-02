package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ============================================================================
// Task Resolution Helpers
// ============================================================================

// TaskInfo holds resolved task information from .agent/tasks/
type TaskInfo struct {
	ID       string // e.g., "07"
	FullID   string // e.g., "TASK-07"
	Title    string // e.g., "Telegram Voice Support"
	Status   string // e.g., "backlog", "complete"
	FilePath string // Full path to task file
}

// resolveTaskID looks up a task number and returns task info
// Input can be "07", "7", "TASK-07", "task 7", etc.
func (h *Handler) resolveTaskID(input string) *TaskInfo {
	// Normalize input - extract just the number
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.TrimPrefix(input, "task-")
	input = strings.TrimPrefix(input, "task ")
	input = strings.TrimPrefix(input, "#")

	// Try to parse as number
	num, err := strconv.Atoi(input)
	if err != nil {
		return nil
	}

	// Format as two-digit for file lookup
	taskNum := fmt.Sprintf("%02d", num)

	// Search for matching task file
	tasksDir := filepath.Join(h.projectPath, ".agent", "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.ToUpper(entry.Name())
		// Match TASK-07-*.md or TASK-7-*.md
		if strings.HasPrefix(name, fmt.Sprintf("TASK-%s-", taskNum)) ||
			strings.HasPrefix(name, fmt.Sprintf("TASK-%d-", num)) {

			filePath := filepath.Join(tasksDir, entry.Name())
			status, title := parseTaskFile(filePath)

			return &TaskInfo{
				ID:       taskNum,
				FullID:   fmt.Sprintf("TASK-%s", taskNum),
				Title:    title,
				Status:   status,
				FilePath: filePath,
			}
		}
	}

	return nil
}

// loadTaskDescription reads the full task description from the file
func (h *Handler) loadTaskDescription(taskInfo *TaskInfo) string {
	if taskInfo == nil || taskInfo.FilePath == "" {
		return ""
	}

	data, err := os.ReadFile(taskInfo.FilePath)
	if err != nil {
		return ""
	}

	return string(data)
}

// resolveTaskFromDescription extracts task ID from descriptions like:
// "Start task 07", "task 7", "07", "run 25", "execute task-07"
func (h *Handler) resolveTaskFromDescription(description string) *TaskInfo {
	desc := strings.ToLower(strings.TrimSpace(description))

	// Patterns to extract task number
	patterns := []string{
		`(?i)(?:start|run|execute|do)\s+(?:task[- ]?)?(\d+)`,
		`(?i)task[- ]?(\d+)`,
		`^(\d+)$`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(desc); len(matches) > 1 {
			return h.resolveTaskID(matches[1])
		}
	}

	return nil
}
