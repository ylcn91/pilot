package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatTaskDocRendersTitleAndSections(t *testing.T) {
	doc := &TaskDoc{
		ID:                 "GH-42",
		Title:              "feat(executor): persist run decision log",
		Description:        "Persist decisions so retries stay anchored.",
		AcceptanceCriteria: []string{"Title is rendered", "Sections render only when non-empty"},
		Constraints:        []string{"Edit only docs.go and runner.go"},
		DecisionLog: []DecisionEntry{
			{Date: "2026-06-02", Decision: "Use a markdown table", Reasoning: "Human-readable | grep-able", Alternatives: "JSON sidecar"},
		},
		OpenQuestions: []string{"Should constraints flow from the Task struct?"},
		KeyFiles:      []string{"internal/executor/docs.go"},
		PlannedSteps:  []string{"Add fields", "Rewrite formatter"},
	}

	out := formatTaskDoc(doc)

	if !strings.HasPrefix(out, "# feat(executor): persist run decision log\n") {
		t.Errorf("expected H1 to use Title, got:\n%s", out)
	}
	for _, want := range []string{
		"## Problem",
		"## Acceptance Criteria",
		"## Decisions Log",
		"| Date | Decision | Reasoning | Alternatives |",
		"Human-readable \\| grep-able", // pipe escaped inside a cell
		"## Constraints",
		"## Open Questions",
		"## Key Files",
		"## Planned Steps",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestFormatTaskDocOmitsEmptySections(t *testing.T) {
	doc := &TaskDoc{
		ID:                 "GH-7",
		Description:        "Minimal task.",
		AcceptanceCriteria: []string{"Builds"},
	}

	out := formatTaskDoc(doc)

	// Falls back to ID heading when Title is empty.
	if !strings.HasPrefix(out, "# GH-7\n") {
		t.Errorf("expected H1 to fall back to ID, got:\n%s", out)
	}
	for _, absent := range []string{"## Decisions Log", "## Constraints", "## Open Questions", "## Key Files", "## Planned Steps"} {
		if strings.Contains(out, absent) {
			t.Errorf("expected %q to be omitted for empty slice, got:\n%s", absent, out)
		}
	}
}

func TestAppendDecisionCreatesAndAppends(t *testing.T) {
	agentPath := filepath.Join(t.TempDir(), ".agent")
	task := &Task{
		ID:          "GH-99",
		Title:       "feat(x): thing",
		Description: "do the thing",
	}
	if err := CreateTaskDoc(agentPath, task); err != nil {
		t.Fatalf("CreateTaskDoc failed: %v", err)
	}

	first := DecisionEntry{Date: "2026-06-02", Decision: "A", Reasoning: "because A", Alternatives: "B"}
	if err := AppendDecision(agentPath, task.ID, first); err != nil {
		t.Fatalf("AppendDecision (create) failed: %v", err)
	}
	second := DecisionEntry{Decision: "C", Reasoning: "because C"}
	if err := AppendDecision(agentPath, task.ID, second); err != nil {
		t.Fatalf("AppendDecision (append) failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(agentPath, "tasks", "gh-99.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)

	if strings.Count(got, "## Decisions Log") != 1 {
		t.Errorf("expected exactly one Decisions Log header, got:\n%s", got)
	}
	if !strings.Contains(got, "| 2026-06-02 | A | because A | B |") {
		t.Errorf("expected first decision row, got:\n%s", got)
	}
	if !strings.Contains(got, "| C | because C |") {
		t.Errorf("expected second decision row, got:\n%s", got)
	}
}

func TestLoadRunDocRoundTrip(t *testing.T) {
	agentPath := filepath.Join(t.TempDir(), ".agent")
	tasksDir := filepath.Join(agentPath, "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}

	doc := &TaskDoc{
		ID:          "GH-55",
		Title:       "feat(x): persist",
		Description: "desc",
		Constraints: []string{"Edit only owned files", "No new deps"},
		DecisionLog: []DecisionEntry{
			{Date: "2026-06-02", Decision: "table over json", Reasoning: "readable", Alternatives: "json"},
		},
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "gh-55.md"), []byte(formatTaskDoc(doc)), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadRunDoc(agentPath, "GH-55")
	if err != nil {
		t.Fatalf("loadRunDoc failed: %v", err)
	}
	if len(loaded.Constraints) != 2 || loaded.Constraints[0] != "Edit only owned files" {
		t.Errorf("constraints not parsed: %#v", loaded.Constraints)
	}
	if len(loaded.DecisionLog) != 1 || loaded.DecisionLog[0].Decision != "table over json" || loaded.DecisionLog[0].Reasoning != "readable" {
		t.Errorf("decision log not parsed: %#v", loaded.DecisionLog)
	}
}

func TestUpdateFeatureMatrix(t *testing.T) {
	tmpDir := t.TempDir()
	agentPath := filepath.Join(tmpDir, ".agent")
	systemPath := filepath.Join(agentPath, "system")

	if err := os.MkdirAll(systemPath, 0755); err != nil {
		t.Fatal(err)
	}

	matrixPath := filepath.Join(systemPath, "FEATURE-MATRIX.md")
	baseContent := `# Pilot Feature Matrix

**Last Updated:** 2026-02-14 (v1.0.0)

## Core Execution

| Feature | Status | Package | CLI Command | Config Key | Notes |
|---------|--------|---------|-------------|------------|-------|
| Task execution | ✅ | executor | ` + "`pilot task`" + ` | - | Claude Code subprocess |

## Intelligence

| Feature | Status | Package | CLI Command | Config Key | Notes |
|---------|--------|---------|-------------|------------|-------|
| Complexity detection | ✅ | executor | - | - | Haiku LLM classifier |
`
	if err := os.WriteFile(matrixPath, []byte(baseContent), 0644); err != nil {
		t.Fatal(err)
	}

	task := &Task{
		ID:    "GH-1388",
		Title: "feat(executor): update Navigator docs after task execution",
	}

	if err := UpdateFeatureMatrix(agentPath, task, "v1.10.0"); err != nil {
		t.Fatalf("UpdateFeatureMatrix failed: %v", err)
	}

	content, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	contentStr := string(content)

	// Feature name present
	if !strings.Contains(contentStr, "Update Navigator docs") {
		t.Errorf("Expected feature name not found. Content:\n%s", contentStr)
	}

	// Version present
	if !strings.Contains(contentStr, "v1.10.0") {
		t.Errorf("Expected version v1.10.0 not found")
	}

	// Task ID referenced
	if !strings.Contains(contentStr, "GH-1388") {
		t.Errorf("Expected task ID GH-1388 not found")
	}

	// New row inserted BEFORE ## Intelligence (in Core Execution table)
	newRowIdx := strings.Index(contentStr, "Update Navigator docs")
	intellIdx := strings.Index(contentStr, "## Intelligence")
	if newRowIdx > intellIdx {
		t.Errorf("New row inserted after ## Intelligence, should be before")
	}

	// New row inserted AFTER the existing Core Execution data row
	taskExecIdx := strings.Index(contentStr, "Task execution")
	if newRowIdx < taskExecIdx {
		t.Errorf("New row inserted before existing data row, should be after")
	}
}

func TestUpdateFeatureMatrixNoIntelligenceSection(t *testing.T) {
	tmpDir := t.TempDir()
	agentPath := filepath.Join(tmpDir, ".agent")
	systemPath := filepath.Join(agentPath, "system")

	if err := os.MkdirAll(systemPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Matrix with Core Execution only, no other sections
	matrixPath := filepath.Join(systemPath, "FEATURE-MATRIX.md")
	baseContent := `# Pilot Feature Matrix

## Core Execution

| Feature | Status | Package | CLI Command | Config Key | Notes |
|---------|--------|---------|-------------|------------|-------|
| Task execution | ✅ | executor | ` + "`pilot task`" + ` | - | Claude Code subprocess |
`
	if err := os.WriteFile(matrixPath, []byte(baseContent), 0644); err != nil {
		t.Fatal(err)
	}

	task := &Task{
		ID:    "GH-2000",
		Title: "feat(gateway): add health endpoint",
	}

	if err := UpdateFeatureMatrix(agentPath, task, "v2.0.0"); err != nil {
		t.Fatalf("UpdateFeatureMatrix failed: %v", err)
	}

	content, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	contentStr := string(content)

	if !strings.Contains(contentStr, "Add health endpoint") {
		t.Errorf("Expected feature name not found. Content:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "v2.0.0") {
		t.Errorf("Expected version v2.0.0 not found")
	}
}

func TestUpdateFeatureMatrixMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	agentPath := filepath.Join(tmpDir, ".agent")

	task := &Task{
		ID:    "GH-1388",
		Title: "feat(executor): update Navigator docs",
	}

	// Should not fail, just log warning and continue
	err := UpdateFeatureMatrix(agentPath, task, "v1.10.0")
	if err != nil {
		t.Errorf("Expected UpdateFeatureMatrix to handle missing file gracefully, got error: %v", err)
	}
}

func TestExtractFeatureName(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{
			title:    "feat(executor): update Navigator docs after task execution",
			expected: "Update Navigator docs after task execution",
		},
		{
			title:    "feat(auth): add OAuth provider integration",
			expected: "Add OAuth provider integration",
		},
		{
			title:    "fix(api): handle nil response",
			expected: "Handle nil response",
		},
		{
			title:    "Simple title without prefix",
			expected: "Simple title without prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			result := extractFeatureName(tt.title)
			if result != tt.expected {
				t.Errorf("extractFeatureName(%q) = %q, expected %q", tt.title, result, tt.expected)
			}
		})
	}
}

// TestSanitizeFilename verifies GH-2377 fix: titles containing path
// separators (/, \) and other filesystem-unsafe chars (:|<>?*") must not
// leak into os.WriteFile paths as subdirectory traversals.
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"spaces lowercased", "Hello World", "hello-world"},
		{"forward slash", "fix(jira): migrate /rest/api/2 to /rest/api/3", "fix(jira)-migrate-rest-api-2-to-rest-api-3"},
		{"pipe and slash", "fix: /rest/api/2|3/search", "fix-rest-api-2-3-search"},
		{"colon", "task: do a thing", "task-do-a-thing"},
		{"backslash", "path\\to\\thing", "path-to-thing"},
		{"angle brackets", "<foo> vs <bar>", "foo-vs-bar"},
		{"question mark and star", "what? *now*", "what-now"},
		{"quote", `say "hi"`, "say-hi"},
		{"trim leading trailing dashes", "/leading/", "leading"},
		{"collapse consecutive dashes", "a // b", "a-b"},
		{"plain simple", "simple-id", "simple-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			if strings.ContainsAny(got, `/\:|<>?*"`) {
				t.Errorf("sanitizeFilename(%q) = %q still contains unsafe chars", tt.input, got)
			}
		})
	}
}
