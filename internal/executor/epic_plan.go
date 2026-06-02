package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// numberedListRegex matches numbered patterns: "1. ", "1) ", "Step 1:", "Phase 1:", "**1.", etc.
// Allows optional markdown bold markers (**) before the number (GH-490 fix).
// Also handles markdown heading prefixes (### 1.), dash/asterisk bullets (- 1., * 1.),
// and combinations like "- **1. Title**" or "### Step 1: Title" (GH-542 fix).
// Used by parseSubtasks as the regex fallback in the parsing pipeline:
//
//	PlanEpic → parseSubtasksWithFallback → SubtaskParser (Haiku API) → parseSubtasks (regex)
var numberedListRegex = regexp.MustCompile(`(?mi)^(?:\s*)(?:#{1,6}\s+)?(?:[-*]\s+)?(?:\*{0,2})(?:step|phase|task)?\s*(\d+)[.):]\s*(.+)`)

// PlanEpic runs Claude Code in planning mode to break an epic into subtasks.
// Returns an EpicPlan with 3-5 sequential subtasks.
// executionPath may differ from task.ProjectPath when using worktree isolation (GH-968).
func (r *Runner) PlanEpic(ctx context.Context, task *Task, executionPath string) (*EpicPlan, error) {
	// Build planning prompt. agentDir mirrors the executor's priming source so
	// the planner sees the same project context and SOP hints (GH worktree-safe:
	// derived from executionPath, not task.ProjectPath).
	agentDir := filepath.Join(executionPath, ".agent")
	prompt := buildPlanningPrompt(task, agentDir)

	// Get claude command from config or use default
	claudeCmd := "claude"
	if r.config != nil && r.config.ClaudeCode != nil && r.config.ClaudeCode.Command != "" {
		claudeCmd = r.config.ClaudeCode.Command
	}

	// GH-2432: Planning gets Opus for stronger reasoning; execution stays on
	// Sonnet (set via the regular runner path). The model is also exported via
	// ANTHROPIC_MODEL because Pilot's global env may otherwise win on Node's
	// last-write lookup inside Claude Code (see backend_claudecode.go).
	planningModel := "claude-opus-4-7"
	if r.config != nil && r.config.Planning != nil && r.config.Planning.Model != "" {
		planningModel = r.config.Planning.Model
	}

	// Run Claude Code with --print flag for planning. Restrict tools to
	// read-only — planning must not write code.
	args := []string{
		"--print", "-p", prompt,
		"--model", planningModel,
		"--allowedTools", strings.Join(DefaultAllowedToolsPlanning(), ","),
	}

	cmd := exec.CommandContext(ctx, claudeCmd, args...)
	cmd.Env = append(os.Environ(), "ANTHROPIC_MODEL="+planningModel)

	// Set working directory - use executionPath which respects worktree isolation
	if executionPath != "" {
		cmd.Dir = executionPath
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.log.Debug("Running Claude Code planning",
		"task_id", task.ID,
		"command", claudeCmd,
		"args", args,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude planning failed: %w (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		return nil, fmt.Errorf("claude planning returned empty output")
	}

	// Parse subtasks: tries Haiku structured extraction first, falls back to regex.
	// See parseSubtasksWithFallback in subtask_parser.go for the fallback chain.
	subtasks := parseSubtasksWithFallback(r.subtaskParser, output)
	if len(subtasks) == 0 {
		return nil, fmt.Errorf("no subtasks found in planning output")
	}

	// Validate and fix subtask titles: enforce conventional-commits format,
	// reject placeholders, re-prompt via LLM or fall back to parent type/scope.
	subtasks = validateAndFixSubtaskTitles(ctx, subtasks, task, r.subtaskParser, r.log)

	return &EpicPlan{
		ParentTask: task,
		Subtasks:   subtasks,
		PlanOutput: output,
	}, nil
}

// buildPlanningPrompt creates the prompt for epic planning. agentDir points at
// the project's .agent/ directory so the planner is primed with the same project
// context and SOP hints the executor receives (loadProjectContext /
// findRelevantSOPs in prompt_builder.go). Empty/missing priming is skipped.
func buildPlanningPrompt(task *Task, agentDir string) string {
	var sb strings.Builder

	sb.WriteString("You are a software architect planning an implementation.\n\n")
	sb.WriteString("Break down this epic task into 3-5 sequential subtasks that can each be completed independently.\n")
	sb.WriteString("Each subtask should be a concrete, implementable unit of work.\n")
	sb.WriteString("Do NOT propose abstractions, layers, or generalizations the ticket does not imply — plan only the work the task actually requires.\n\n")

	// Prime the planner with project context and relevant SOPs, mirroring the
	// executor's priming so plans align with existing architecture.
	if agentDir != "" {
		if projectCtx := loadProjectContext(agentDir); projectCtx != "" {
			sb.WriteString("## Project Context\n\n")
			sb.WriteString(projectCtx)
			sb.WriteString("\n\n")
		}
		if sops := findRelevantSOPs(agentDir, task.Title+" "+task.Description); len(sops) > 0 {
			sb.WriteString("## Relevant SOPs\n\n")
			sb.WriteString("Consider these when planning:\n")
			for _, sop := range sops {
				sb.WriteString(fmt.Sprintf("- `.agent/%s`\n", sop))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("## CRITICAL: Subtask Title Format\n\n")
	sb.WriteString("Every subtask title MUST follow the conventional-commits format:\n\n")
	sb.WriteString("  type(scope): description\n\n")
	sb.WriteString("Accepted types: feat, fix, chore, refactor, test, docs, perf, build, ci, style\n")
	sb.WriteString("Format templates (these are placeholders, NOT real subtasks — do NOT copy them verbatim):\n")
	sb.WriteString("  feat(SCOPE): IMPERATIVE_SUMMARY\n")
	sb.WriteString("  fix(SCOPE): WHAT_IS_BEING_FIXED\n")
	sb.WriteString("  chore(SCOPE): MAINTENANCE_ACTION\n\n")
	sb.WriteString("Replace SCOPE and the ALL_CAPS slots with terms drawn from the actual task being planned.\n")
	sb.WriteString("Do NOT emit titles like \"GH-123: Subtask 1\" or plain action phrases without a type prefix.\n\n")

	sb.WriteString("## CRITICAL: Avoid Single-Package Splits\n\n")
	sb.WriteString("If all the work lives in one package or directory (e.g., all files in `cmd/pilot/`),\n")
	sb.WriteString("DO NOT split into separate subtasks. Instead, return a SINGLE subtask with the full scope.\n")
	sb.WriteString("Splitting work within the same package causes merge conflicts when subtasks execute in parallel.\n")
	sb.WriteString("Only split when subtasks genuinely touch DIFFERENT packages or directories.\n\n")

	sb.WriteString("## Task to Plan\n\n")
	sb.WriteString(fmt.Sprintf("**Title:** %s\n\n", task.Title))
	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:**\n%s\n\n", task.Description))
	}
	if len(task.Labels) > 0 {
		sb.WriteString(fmt.Sprintf("Labels: %s\n\n", strings.Join(task.Labels, ", ")))
	}

	sb.WriteString("## Output Format\n\n")
	sb.WriteString("List each subtask with a number, title, and description:\n\n")
	sb.WriteString("1. **Subtask title** - Description of what needs to be done\n")
	sb.WriteString("2. **Next subtask** - Its description\n")
	sb.WriteString("...\n\n")

	sb.WriteString("Focus on:\n")
	sb.WriteString("- Clear boundaries between subtasks\n")
	sb.WriteString("- Logical ordering (dependencies flow naturally)\n")
	sb.WriteString("- Each subtask should be testable/verifiable\n")
	sb.WriteString("- Include any setup/infrastructure subtasks first\n")
	sb.WriteString("- NEVER split work that belongs to the same Go package or directory into separate subtasks\n")

	return sb.String()
}

// consolidateEpicPlan merges the original task description with the planned subtasks
// into a single description for non-decomposed execution. The executor gets the full
// implementation plan but executes it as one unit on one branch.
func consolidateEpicPlan(originalDesc string, subtasks []PlannedSubtask) string {
	var sb strings.Builder
	sb.WriteString(originalDesc)
	sb.WriteString("\n\n## Planned Steps (execute all in sequence)\n\n")
	for _, st := range subtasks {
		sb.WriteString(fmt.Sprintf("%d. **%s**", st.Order, st.Title))
		if st.Description != "" {
			sb.WriteString(" — ")
			sb.WriteString(st.Description)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// isSinglePackageScope checks whether all planned subtasks reference files within
// the same Go package or directory. When true, creating separate GitHub issues
// would cause merge conflicts because each sub-issue branches from main independently.
//
// Detection strategy:
// 1. Extract all file paths mentioned across subtask titles and descriptions
// 2. Compute unique parent directories
// 3. If only 1 directory (or 0 files found), consider it single-package scope
//
// GH-1265: This prevents the "serial conflict cascade" bug where N sub-issues
// all touching cmd/pilot/ create N branches from main, each redeclaring shared types.
func isSinglePackageScope(subtasks []PlannedSubtask, taskDescription string) bool {
	// Collect all text to scan for file references
	var allText strings.Builder
	allText.WriteString(taskDescription)
	allText.WriteString("\n")
	for _, st := range subtasks {
		allText.WriteString(st.Title)
		allText.WriteString("\n")
		allText.WriteString(st.Description)
		allText.WriteString("\n")
	}

	dirs := extractUniqueDirectories(allText.String())

	// If we found file references and they all point to 1 directory → single package
	if len(dirs) == 1 {
		return true
	}

	// If no file references found, use heuristic: check if subtask titles suggest
	// the same component (e.g., all mention "onboard", "dashboard", "config")
	if len(dirs) == 0 {
		return detectSameComponentFromTitles(subtasks)
	}

	return false
}

// extractUniqueDirectories finds file paths in text and returns their unique parent directories.
// Delegates to the shared ExtractDirectoriesFromText (scope.go) for reuse across packages.
func extractUniqueDirectories(text string) map[string]bool {
	return ExtractDirectoriesFromText(text)
}

// detectSameComponentFromTitles checks if subtask titles all reference the same component.
// Uses a simple heuristic: extract the most common significant word from titles.
// If one word appears in >80% of subtask titles, it's likely single-scope.
func detectSameComponentFromTitles(subtasks []PlannedSubtask) bool {
	if len(subtasks) < 2 {
		return false
	}

	// Count word frequency across titles
	wordCounts := make(map[string]int)
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "for": true,
		"to": true, "in": true, "of": true, "with": true, "from": true, "by": true,
		"add": true, "create": true, "implement": true, "update": true, "fix": true,
		"setup": true, "set": true, "up": true, "new": true, "test": true, "tests": true,
		// conventional-commit type prefixes are not component names
		"feat": true, "chore": true, "refactor": true, "docs": true, "perf": true,
		"build": true, "ci": true, "style": true, "revert": true,
	}

	for _, st := range subtasks {
		words := strings.Fields(strings.ToLower(st.Title))
		seen := make(map[string]bool) // dedupe within a single title
		for _, w := range words {
			w = strings.Trim(w, ".,:-()[]\"'`*")
			if len(w) < 3 || stopWords[w] {
				continue
			}
			if !seen[w] {
				wordCounts[w]++
				seen[w] = true
			}
		}
	}

	// Check if any significant word appears in >80% of titles
	threshold := int(float64(len(subtasks)) * 0.8)
	for _, count := range wordCounts {
		if count >= threshold {
			return true
		}
	}

	return false
}

// parseSubtasks extracts subtasks from Claude's planning output using regex.
// This is the fallback parser when Haiku API is unavailable (see subtask_parser.go).
// Looks for numbered patterns: "1. Title - Description", "Step 1: Title", "**1. Title**"
func parseSubtasks(output string) []PlannedSubtask {
	var subtasks []PlannedSubtask
	seenOrders := make(map[int]bool)

	scanner := bufio.NewScanner(strings.NewReader(output))
	var currentSubtask *PlannedSubtask
	var descriptionLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// Try to match numbered list patterns
		matches := numberedListRegex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			// Save previous subtask if exists
			if currentSubtask != nil {
				finalizeSubtask(currentSubtask, descriptionLines)
				if currentSubtask.Title != "" && !seenOrders[currentSubtask.Order] {
					subtasks = append(subtasks, *currentSubtask)
					seenOrders[currentSubtask.Order] = true
				}
			}

			order := 0
			_, _ = fmt.Sscanf(matches[1], "%d", &order)

			// Extract title and possibly inline description
			titleAndDesc := strings.TrimSpace(matches[2])
			title, desc := splitTitleDescription(titleAndDesc)

			currentSubtask = &PlannedSubtask{
				Title:       title,
				Description: desc,
				Order:       order,
			}
			descriptionLines = nil
			continue
		}

		// Accumulate description lines for current subtask
		if currentSubtask != nil && strings.TrimSpace(line) != "" {
			// Skip markdown headers that might be formatting
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				descriptionLines = append(descriptionLines, strings.TrimSpace(line))
			}
		}
	}

	// Save last subtask
	if currentSubtask != nil {
		finalizeSubtask(currentSubtask, descriptionLines)
		if currentSubtask.Title != "" && !seenOrders[currentSubtask.Order] {
			subtasks = append(subtasks, *currentSubtask)
		}
	}

	return subtasks
}

// splitTitleDescription splits "**Title** - Description" or "Title: Description" patterns.
func splitTitleDescription(s string) (title, description string) {
	// Remove markdown bold markers
	s = strings.ReplaceAll(s, "**", "")

	// Try common separators (em-dash first since Claude often uses it)
	separators := []string{" — ", " - ", ": ", " – "}
	for _, sep := range separators {
		if idx := strings.Index(s, sep); idx > 0 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+len(sep):])
		}
	}

	// No separator found, entire string is title
	return strings.TrimSpace(s), ""
}

// finalizeSubtask combines inline description with accumulated description lines.
func finalizeSubtask(subtask *PlannedSubtask, lines []string) {
	if len(lines) == 0 {
		return
	}

	accumulated := strings.TrimSpace(strings.Join(lines, "\n"))
	if subtask.Description == "" {
		subtask.Description = accumulated
	} else {
		// Prepend inline description to accumulated lines
		subtask.Description = subtask.Description + "\n" + accumulated
	}
}
