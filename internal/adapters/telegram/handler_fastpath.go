package telegram

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ============================================================================
// Fast Path Handlers
// ============================================================================

// tryFastAnswer attempts to answer common questions without spawning Claude Code
// Returns empty string if question needs full Claude processing
func (h *Handler) tryFastAnswer(question string) string {
	q := strings.ToLower(question)

	switch {
	case containsAny(q, "issues", "tasks", "backlog", "todo list", "what to do"):
		return h.fastListTasks()
	case containsAny(q, "status", "progress", "current state"):
		return h.fastReadStatus()
	case containsAny(q, "todos", "fixmes", "todo", "fixme"):
		return h.fastGrepTodos()
	}

	return "" // Fall back to Claude
}

// containsAny returns true if s contains any of the substrings
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// fastListTasks lists tasks from .agent/tasks/ directory
func (h *Handler) fastListTasks() string {
	tasksDir := filepath.Join(h.projectPath, ".agent", "tasks")

	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return "" // Fall back to Claude
	}

	type taskInfo struct {
		num   string
		title string
	}
	var pending, inProgress, completed []taskInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(tasksDir, entry.Name())
		status, title := parseTaskFile(filePath)

		// Extract task number (e.g., "07" from "TASK-07-telegram-voice.md")
		taskNum := extractTaskNumber(entry.Name())
		if taskNum == "" {
			continue
		}

		info := taskInfo{num: taskNum, title: title}

		switch {
		case strings.Contains(status, "complete") || strings.Contains(status, "done") || strings.Contains(status, "✅"):
			completed = append(completed, info)
		case strings.Contains(status, "progress") || strings.Contains(status, "🚧") || strings.Contains(status, "wip"):
			inProgress = append(inProgress, info)
		default:
			pending = append(pending, info)
		}
	}

	if len(pending)+len(inProgress)+len(completed) == 0 {
		return "" // No tasks found, fall back to Claude
	}

	var sb strings.Builder

	// In Progress
	if len(inProgress) > 0 {
		sb.WriteString("In Progress\n")
		for _, t := range inProgress {
			sb.WriteString(fmt.Sprintf("%s: %s\n", t.num, t.title))
		}
		sb.WriteString("\n")
	}

	// Backlog - show first 5
	if len(pending) > 0 {
		sb.WriteString("Backlog\n")
		showCount := min(5, len(pending))
		for i := 0; i < showCount; i++ {
			sb.WriteString(fmt.Sprintf("%s: %s\n", pending[i].num, pending[i].title))
		}
		if len(pending) > 5 {
			sb.WriteString(fmt.Sprintf("_+%d more planned_\n", len(pending)-5))
		}
		sb.WriteString("\n")
	}

	// Recently done - show last 2
	if len(completed) > 0 {
		sb.WriteString("Recently done\n")
		showCount := min(2, len(completed))
		start := len(completed) - showCount
		for i := start; i < len(completed); i++ {
			sb.WriteString(fmt.Sprintf("%s: %s\n", completed[i].num, completed[i].title))
		}
		sb.WriteString("\n")
	}

	// Progress bar
	total := len(pending) + len(inProgress) + len(completed)
	doneCount := len(completed)
	percent := 0
	if total > 0 {
		percent = (doneCount * 100) / total
	}
	sb.WriteString(fmt.Sprintf("Progress: %s %d%%", makeProgressBar(percent), percent))

	return sb.String()
}

// extractTaskNumber gets "07" from "TASK-07-name.md"
func extractTaskNumber(filename string) string {
	// Remove .md
	name := strings.TrimSuffix(filename, ".md")

	// Handle TASK-XX format
	if strings.HasPrefix(strings.ToUpper(name), "TASK-") {
		rest := name[5:] // After "TASK-"
		// Find end of number
		numEnd := 0
		for i, c := range rest {
			if c >= '0' && c <= '9' {
				numEnd = i + 1
			} else {
				break
			}
		}
		if numEnd > 0 {
			return rest[:numEnd]
		}
	}
	return ""
}

// makeProgressBar creates a text progress bar
func makeProgressBar(percent int) string {
	filled := percent / 5 // 20 chars total
	empty := 20 - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// parseTaskFile reads a task file and extracts status and title
func parseTaskFile(path string) (status, title string) {
	file, err := os.Open(path)
	if err != nil {
		return "pending", ""
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() && lineCount < 15 {
		line := scanner.Text()
		lineCount++

		// Extract title from "# TASK-XX: Title" or first heading
		if strings.HasPrefix(line, "# ") && title == "" {
			title = strings.TrimPrefix(line, "# ")
			// Remove task ID prefix if present
			if idx := strings.Index(title, ":"); idx != -1 && idx < 20 {
				title = strings.TrimSpace(title[idx+1:])
			}
		}

		// Extract status from "**Status**: X" or "Status: X"
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "status") {
			if idx := strings.Index(line, ":"); idx != -1 {
				status = strings.ToLower(strings.TrimSpace(line[idx+1:]))
				// Clean up status markers
				status = strings.Trim(status, "*_` ")
				status = strings.ToLower(status)
			}
		}
	}

	if status == "" {
		status = "pending"
	}

	return status, truncateDescription(title, 50)
}

// fastReadStatus reads project status from DEVELOPMENT-README.md
func (h *Handler) fastReadStatus() string {
	readmePath := filepath.Join(h.projectPath, ".agent", "DEVELOPMENT-README.md")

	data, err := os.ReadFile(readmePath)
	if err != nil {
		return "" // Fall back to Claude
	}

	content := string(data)

	// Extract key sections
	var sb strings.Builder
	sb.WriteString("📊 *Project Status*\n\n")

	// Find "Current State" or "Implementation Status" section
	lines := strings.Split(content, "\n")
	inSection := false
	lineCount := 0

	for _, line := range lines {
		lineLower := strings.ToLower(line)

		// Start capturing at relevant sections
		if strings.Contains(lineLower, "current state") ||
			strings.Contains(lineLower, "implementation status") ||
			strings.Contains(lineLower, "active tasks") {
			inSection = true
			sb.WriteString("*" + strings.TrimPrefix(line, "## ") + "*\n")
			continue
		}

		// Stop at next major section
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}

		if inSection {
			// Convert table rows to list items
			if strings.HasPrefix(strings.TrimSpace(line), "|") {
				cells := strings.Split(line, "|")
				if len(cells) >= 3 {
					cell1 := strings.TrimSpace(cells[1])
					cell2 := strings.TrimSpace(cells[2])
					if cell1 != "" && !strings.Contains(cell1, "---") {
						sb.WriteString(fmt.Sprintf("• %s: %s\n", cell1, cell2))
						lineCount++
					}
				}
			} else if strings.TrimSpace(line) != "" {
				sb.WriteString(line + "\n")
				lineCount++
			}

			if lineCount > 20 {
				sb.WriteString("\n_(truncated)_")
				break
			}
		}
	}

	if lineCount == 0 {
		return "" // Nothing found, fall back to Claude
	}

	return sb.String()
}

// fastGrepTodos searches for TODO/FIXME comments in the codebase
func (h *Handler) fastGrepTodos() string {
	var todos []string

	// Walk common source directories
	dirs := []string{"cmd", "internal", "pkg", "src", "orchestrator"}

	for _, dir := range dirs {
		dirPath := filepath.Join(h.projectPath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}

		_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}

			// Only scan Go and Python files
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".py" {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer func() { _ = file.Close() }()

			scanner := bufio.NewScanner(file)
			lineNum := 0

			for scanner.Scan() {
				lineNum++
				line := scanner.Text()
				lineLower := strings.ToLower(line)

				if strings.Contains(lineLower, "todo") || strings.Contains(lineLower, "fixme") {
					relPath, _ := filepath.Rel(h.projectPath, path)
					// Clean up the line
					comment := strings.TrimSpace(line)
					comment = strings.TrimPrefix(comment, "//")
					comment = strings.TrimPrefix(comment, "#")
					comment = strings.TrimSpace(comment)

					todos = append(todos, fmt.Sprintf("• %s:%d %s", relPath, lineNum, truncateDescription(comment, 60)))

					if len(todos) >= 15 {
						return filepath.SkipAll
					}
				}
			}
			return nil
		})

		if len(todos) >= 15 {
			break
		}
	}

	if len(todos) == 0 {
		return "✨ No TODOs or FIXMEs found in the codebase!"
	}

	// Sort by path for readability
	sort.Strings(todos)

	var sb strings.Builder
	sb.WriteString("📝 TODOs & FIXMEs\n\n")
	for _, todo := range todos {
		sb.WriteString(todo + "\n")
	}

	if len(todos) >= 15 {
		sb.WriteString("\n_(showing first 15)_")
	}

	return sb.String()
}
