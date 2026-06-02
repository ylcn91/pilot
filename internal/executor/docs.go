package executor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TaskDoc represents a task documentation file
type TaskDoc struct {
	ID                 string
	Title              string
	Description        string
	AcceptanceCriteria []string
	CreatedAt          time.Time
	// Constraints captures hard limits the implementation must respect.
	Constraints []string
	// DecisionLog records decisions made during the run so context survives retries.
	DecisionLog []DecisionEntry
	// OpenQuestions lists unresolved ambiguities surfaced during planning/execution.
	OpenQuestions []string
	// KeyFiles lists files central to the change.
	KeyFiles []string
	// PlannedSteps lists the intended implementation steps.
	PlannedSteps []string
}

// DecisionEntry is a single row in a task's Decisions Log.
type DecisionEntry struct {
	Date         string
	Decision     string
	Reasoning    string
	Alternatives string
}

// CreateTaskDoc generates a task documentation file in .agent/tasks/
func CreateTaskDoc(agentPath string, task *Task) error {
	tasksDir := filepath.Join(agentPath, "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.md", sanitizeFilename(task.ID))
	path := filepath.Join(tasksDir, filename)

	doc := &TaskDoc{
		ID:                 task.ID,
		Title:              task.Title,
		Description:        task.Description,
		AcceptanceCriteria: task.AcceptanceCriteria,
		CreatedAt:          time.Now(),
	}
	content := formatTaskDoc(doc)
	return os.WriteFile(path, []byte(content), 0644)
}

// ArchiveTaskDoc moves completed task to archive/
func ArchiveTaskDoc(agentPath, taskID string) error {
	tasksDir := filepath.Join(agentPath, "tasks")
	archiveDir := filepath.Join(tasksDir, "archive")

	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}

	src := filepath.Join(tasksDir, fmt.Sprintf("%s.md", sanitizeFilename(taskID)))
	dst := filepath.Join(archiveDir, fmt.Sprintf("%s.md", sanitizeFilename(taskID)))

	return os.Rename(src, dst)
}

// formatTaskDoc generates markdown content for task doc.
// New sections (Decisions Log, Constraints, Open Questions, Key Files,
// Planned Steps) render only when their slice is non-empty so docs created
// from a bare Task stay byte-stable with the original layout.
func formatTaskDoc(doc *TaskDoc) string {
	var sb strings.Builder

	// H1 uses the human title when present, falling back to the ID so docs
	// generated before titles were threaded through stay valid.
	heading := doc.Title
	if heading == "" {
		heading = doc.ID
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", heading))

	created := doc.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	sb.WriteString(fmt.Sprintf("**Created:** %s\n\n", created.Format("2006-01-02")))

	if doc.Title != "" {
		sb.WriteString(fmt.Sprintf("`%s`\n\n", doc.ID))
	}

	sb.WriteString("## Problem\n\n")
	sb.WriteString(doc.Description)
	sb.WriteString("\n\n## Acceptance Criteria\n\n")
	for _, ac := range doc.AcceptanceCriteria {
		sb.WriteString(fmt.Sprintf("- [ ] %s\n", ac))
	}

	if len(doc.DecisionLog) > 0 {
		sb.WriteString("\n## Decisions Log\n\n")
		sb.WriteString("| Date | Decision | Reasoning | Alternatives |\n")
		sb.WriteString("|------|----------|-----------|--------------|\n")
		for _, d := range doc.DecisionLog {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				escapeTableCell(d.Date),
				escapeTableCell(d.Decision),
				escapeTableCell(d.Reasoning),
				escapeTableCell(d.Alternatives),
			))
		}
	}

	if len(doc.Constraints) > 0 {
		sb.WriteString("\n## Constraints\n\n")
		for _, c := range doc.Constraints {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}

	if len(doc.OpenQuestions) > 0 {
		sb.WriteString("\n## Open Questions\n\n")
		for _, q := range doc.OpenQuestions {
			sb.WriteString(fmt.Sprintf("- %s\n", q))
		}
	}

	if len(doc.KeyFiles) > 0 {
		sb.WriteString("\n## Key Files\n\n")
		for _, f := range doc.KeyFiles {
			sb.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
	}

	if len(doc.PlannedSteps) > 0 {
		sb.WriteString("\n## Planned Steps\n\n")
		for i, s := range doc.PlannedSteps {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
	}

	return sb.String()
}

// escapeTableCell makes a value safe to embed in a markdown table cell by
// escaping pipes and collapsing newlines that would otherwise break the row.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}

// AppendDecision appends a single row to a task doc's Decisions Log. It is
// best-effort: it creates the table header when absent and writes the row even
// if the doc has no other sections yet, so retries can persist decision context
// without a full rewrite.
func AppendDecision(agentPath, taskID string, entry DecisionEntry) error {
	path := filepath.Join(agentPath, "tasks", fmt.Sprintf("%s.md", sanitizeFilename(taskID)))

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if entry.Date == "" {
		entry.Date = time.Now().Format("2006-01-02")
	}
	row := fmt.Sprintf("| %s | %s | %s | %s |\n",
		escapeTableCell(entry.Date),
		escapeTableCell(entry.Decision),
		escapeTableCell(entry.Reasoning),
		escapeTableCell(entry.Alternatives),
	)

	text := string(content)
	if strings.Contains(text, "## Decisions Log") {
		// Append after the last existing table row in the file.
		out := strings.TrimRight(text, "\n") + "\n" + strings.TrimRight(row, "\n") + "\n"
		return os.WriteFile(path, []byte(out), 0644)
	}

	section := "\n## Decisions Log\n\n" +
		"| Date | Decision | Reasoning | Alternatives |\n" +
		"|------|----------|-----------|--------------|\n" +
		row
	out := strings.TrimRight(text, "\n") + "\n" + section
	return os.WriteFile(path, []byte(out), 0644)
}

// loadRunDoc reads a task doc and extracts the persisted Constraints and
// Decisions Log so they can be re-injected into a retry/continuation prompt.
// It is best-effort: a missing file or absent sections yield empty slices and
// a nil error so callers can inject conditionally without branching on errors.
func loadRunDoc(agentPath, taskID string) (*TaskDoc, error) {
	path := filepath.Join(agentPath, "tasks", fmt.Sprintf("%s.md", sanitizeFilename(taskID)))
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	doc := &TaskDoc{ID: taskID}
	lines := strings.Split(string(content), "\n")

	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		switch section {
		case "Constraints":
			if item, ok := strings.CutPrefix(trimmed, "- "); ok {
				doc.Constraints = append(doc.Constraints, item)
			}
		case "Decisions Log":
			if !strings.HasPrefix(trimmed, "|") {
				continue
			}
			cells := splitTableRow(trimmed)
			// Skip the header and separator rows.
			if len(cells) < 4 || cells[0] == "Date" || strings.HasPrefix(cells[0], "---") {
				continue
			}
			doc.DecisionLog = append(doc.DecisionLog, DecisionEntry{
				Date:         cells[0],
				Decision:     cells[1],
				Reasoning:    cells[2],
				Alternatives: cells[3],
			})
		}
	}

	return doc, nil
}

// splitTableRow splits a "| a | b | c |" markdown row into trimmed cells.
func splitTableRow(row string) []string {
	row = strings.Trim(strings.TrimSpace(row), "|")
	parts := strings.Split(row, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// sanitizeFilename converts a human title into a path-safe filename fragment.
// GH-2377: previously only lowercased + replaced spaces, so titles containing
// "/", ":", "|" (common in REST-path or pipe-delimited titles) leaked into
// os.WriteFile and were interpreted as subdirectories, failing marker writes.
func sanitizeFilename(s string) string {
	s = strings.ToLower(s)
	unsafe := []string{"/", "\\", ":", "|", "<", ">", "?", "*", "\""}
	for _, c := range unsafe {
		s = strings.ReplaceAll(s, c, "-")
	}
	s = strings.ReplaceAll(s, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// UpdateFeatureMatrix appends a new feature row to .agent/system/FEATURE-MATRIX.md
// for feature tasks (feat(scope): ...). Skips non-feature commits.
func UpdateFeatureMatrix(agentPath string, task *Task, version string) error {
	featureMatrixPath := filepath.Join(agentPath, "system", "FEATURE-MATRIX.md")

	// Read the file
	content, err := os.ReadFile(featureMatrixPath)
	if err != nil {
		// File doesn't exist or can't be read - log warning but don't fail execution
		slog.Warn("Could not read FEATURE-MATRIX.md", slog.Any("error", err))
		return nil
	}

	lines := strings.Split(string(content), "\n")
	featureName := extractFeatureName(task.Title)
	newRow := fmt.Sprintf("| %s | ✅ | %s | - | - | %s |", featureName, version, task.ID)

	// Strategy: find the first markdown table (## Core Execution), locate the last
	// pipe-prefixed row in that table, and insert after it. This avoids depending
	// on a specific section header like "## Intelligence" as an anchor.
	inCoreTable := false
	lastPipeIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect start of Core Execution table
		if strings.HasPrefix(trimmed, "## Core Execution") {
			inCoreTable = true
			continue
		}

		// Detect end of Core Execution table (next section header)
		if inCoreTable && strings.HasPrefix(trimmed, "## ") {
			inCoreTable = false
			continue
		}

		// Track last data row (pipe-prefixed, not separator row like |---|---|)
		if inCoreTable && strings.HasPrefix(trimmed, "|") && !strings.Contains(trimmed, "---|") {
			lastPipeIdx = i
		}
	}

	if lastPipeIdx >= 0 {
		// Insert after the last data row in Core Execution table
		after := make([]string, len(lines[lastPipeIdx+1:]))
		copy(after, lines[lastPipeIdx+1:])
		result := append(lines[:lastPipeIdx+1], newRow)
		result = append(result, after...)
		return os.WriteFile(featureMatrixPath, []byte(strings.Join(result, "\n")), 0644)
	}

	// Fallback: append to end of file
	lines = append(lines, newRow)
	return os.WriteFile(featureMatrixPath, []byte(strings.Join(lines, "\n")), 0644)
}

// extractFeatureName extracts a clean feature name from the task title
// e.g., "feat(executor): update Navigator docs after task execution" -> "Update Navigator docs"
func extractFeatureName(title string) string {
	// Remove common prefixes like "feat(scope): "
	if idx := strings.Index(title, "):"); idx != -1 {
		title = title[idx+3:]
	}
	// Capitalize first letter if needed
	title = strings.TrimSpace(title)
	if len(title) > 0 {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	return title
}
