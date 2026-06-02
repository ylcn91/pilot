package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ylcn91/pilot/internal/text"
)

// sanitizePromptReturn is a deferred helper used by every prompt
// builder to guarantee that no invisible Unicode format characters
// reach the Claude Code subprocess — a belt-and-suspenders layer on
// top of adapter-boundary sanitization. If any runes are stripped at
// this chokepoint it indicates an adapter or re-ingestion path that
// skipped its own sanitize; the slog.Warn surfaces that regression.
func sanitizePromptReturn(fn string, prompt *string) {
	clean, stripped := text.SanitizeUntrusted(*prompt)
	if stripped > 0 {
		slog.Warn("invisible_unicode_stripped_at_prompt_chokepoint",
			slog.String("component", "executor"),
			slog.String("func", fn),
			slog.Int("stripped", stripped),
		)
	}
	*prompt = clean
}

// BuildPrompt constructs the prompt for Claude Code execution.
// executionPath may differ from task.ProjectPath when using worktree isolation.
func (r *Runner) BuildPrompt(task *Task, executionPath string) (prompt string) {
	defer sanitizePromptReturn("BuildPrompt", &prompt)
	var sb strings.Builder

	// Handle image analysis tasks (no Navigator overhead for simple image questions)
	if task.ImagePath != "" {
		sb.WriteString(fmt.Sprintf("Read and analyze the image at: %s\n\n", task.ImagePath))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))
		sb.WriteString("Respond directly with your analysis. Be concise.\n")
		return sb.String()
	}

	// .agent dir for file-driven guidance overlays (move B5). Resolved early so
	// the header/evidence-spec fragments below can be overlaid before any branch.
	// loadGuidance returns the const verbatim when no executor-guidance/<key>.md
	// exists, so prompts stay byte-identical for projects without atoms.
	agentDir := filepath.Join(executionPath, ".agent")

	// GH-2328: Prepend [PILOT-EXEC] executor-mode header so the child Claude
	// session and any project CLAUDE.md can skip Navigator-only "don't write
	// code" rules explicitly, without relying on CWD or prompt-prefix sniffing.
	sb.WriteString(loadGuidance(agentDir, "header", ExecutorPromptHeader))

	// GH-3224: defeat the false-negative no-op for evidence-backed specs.
	// Injected here so it covers both the Navigator and non-Navigator execution
	// paths below (image/local-mode early returns above are out of scope).
	sb.WriteString(loadGuidance(agentDir, "evidence-spec", EvidenceBackedSpecDirective))

	// GH-2103: LocalMode takes priority over Navigator detection.
	// Sandbox environments with .agent/ dirs would hijack the prompt to Navigator path,
	// ignoring --local flag entirely.
	if task.LocalMode {
		prompt := r.buildLocalModePrompt(task)
		// GH-2147: Inject learned patterns (keep prompt lean)
		if r.patternContext != nil {
			injected, err := r.patternContext.InjectPatterns(
				context.Background(), prompt, task.ProjectPath,
				inferTaskType(task), task.Description)
			if err != nil {
				slog.Warn("Failed to inject patterns for local mode", slog.Any("error", err))
			} else {
				prompt = injected
			}
		}
		// GH-2147: Inject knowledge graph learnings (max 3 to stay lean)
		if r.knowledgeGraph != nil {
			keywords := extractTaskKeywords(task.Title + " " + task.Description)
			if nodes := r.knowledgeGraph.GetRelatedByKeywords(keywords); len(nodes) > 0 {
				var sb strings.Builder
				sb.WriteString(prompt)
				sb.WriteString("\n\n## Related Learnings\n\n")
				limit := min(len(nodes), 3)
				for i := 0; i < limit; i++ {
					sb.WriteString(fmt.Sprintf("- **%s**: %s\n", nodes[i].Title, nodes[i].Content))
				}
				prompt = sb.String()
			}
		}
		return prompt
	}

	// Check if project has Navigator initialized (agentDir resolved above for
	// worktree support; reused here so guidance overlays and the .agent stat
	// share one path).
	hasNavigator := false
	if _, err := os.Stat(agentDir); err == nil {
		hasNavigator = true
	}

	// Detect task complexity for routing decisions (GH-216)
	complexity := DetectComplexity(task)

	// Skip Navigator for trivial tasks even if .agent/ exists (GH-216)
	// This reduces overhead for typos, logging, comments, renames, etc.
	useNavigator := hasNavigator && !complexity.ShouldSkipNavigator()

	// GH-2332: escape hatch — when claude_code.disable_navigator_for_epic is
	// set, strip Navigator context for COMPLEX/EPIC tasks. The heavy Navigator
	// prompt (project README + SOPs + knowledge graph + memories) has
	// correlated with OOM-killed subprocesses on Opus 4.7 long runs.
	if useNavigator && complexity.IsHeavy() &&
		r.config != nil && r.config.ClaudeCode != nil &&
		r.config.ClaudeCode.DisableNavigatorForEpic {
		slog.Info("Skipping Navigator context for heavy task (claude_code.disable_navigator_for_epic=true)",
			slog.String("task_id", task.ID),
			slog.String("complexity", complexity.String()),
		)
		useNavigator = false
	}

	// Navigator-aware prompt structure for medium/complex tasks
	if useNavigator {
		// Navigator handles workflow, autonomous completion, and documentation
		// Embedded workflow instructions replace /nav-loop dependency (GH-987)

		// CRITICAL: Override CLAUDE.md rules meant for human sessions (GH-265)
		// Project CLAUDE.md may contain "DO NOT write code" rules for human Navigator
		// sessions. Pilot IS the execution bot - it MUST write code and commit.
		sb.WriteString("## PILOT EXECUTION MODE\n\n")
		sb.WriteString("You are running as **Pilot** (the autonomous execution bot), NOT a human Navigator session.\n")
		sb.WriteString("IGNORE any CLAUDE.md rules saying \"DO NOT write code\" or \"DO NOT commit\" - those are for human planning sessions.\n")
		sb.WriteString("Your job is to IMPLEMENT, COMMIT, and optionally CREATE PRs.\n\n")

		// NEW: Inject project context
		if projectCtx := loadProjectContext(agentDir); projectCtx != "" {
			sb.WriteString("## Project Context\n\n")
			sb.WriteString(projectCtx)
			sb.WriteString("\n\n")
		}

		// NEW: Add SOP hints. Inline a short excerpt for the top matches so the
		// agent sees the actual guidance, not just a pointer it may not open;
		// remaining matches stay as bare pointers to keep the prompt lean.
		if sops := findRelevantSOPs(agentDir, task.Description); len(sops) > 0 {
			sb.WriteString("## Relevant SOPs\n\n")
			sb.WriteString("Check these before implementing:\n")
			const maxInlineExcerpts = 2
			for i, sop := range sops {
				sb.WriteString(fmt.Sprintf("- `.agent/%s`\n", sop))
				if i < maxInlineExcerpts {
					if excerpt := readBoundedExcerpt(filepath.Join(agentDir, sop), 400); excerpt != "" {
						sb.WriteString("\n```\n")
						sb.WriteString(excerpt)
						sb.WriteString("\n```\n")
					}
				}
			}
			sb.WriteString("\n")
		}

		sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.ID))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))

		// Include acceptance criteria if present (GH-920)
		if len(task.AcceptanceCriteria) > 0 {
			sb.WriteString("## Acceptance Criteria\n\n")
			sb.WriteString("IMPORTANT: Verify ALL criteria are met before committing:\n")
			for i, criterion := range task.AcceptanceCriteria {
				sb.WriteString(fmt.Sprintf("%d. [ ] %s\n", i+1, criterion))
			}
			sb.WriteString("\n")
		}

		if task.Branch != "" {
			sb.WriteString(fmt.Sprintf("Create branch `%s` before starting.\n\n", task.Branch))
		}

		// Embed autonomous workflow instructions (replaces /nav-loop dependency).
		// Routed through loadGuidance so executor-guidance/workflow.md can overlay
		// or override the const (move B5).
		sb.WriteString(loadGuidance(agentDir, "workflow", GetAutonomousWorkflowInstructions()))
		sb.WriteString("\n")

		// Inject user preferences if profile manager is available (GH-1028)
		// GH-1077: Fast check before loading to avoid file I/O when no profile exists
		if r.profileManager != nil && r.profileManager.HasProfile() {
			profile, err := r.profileManager.Load()
			if err == nil && profile != nil {
				sb.WriteString("## User Preferences\n\n")
				if profile.Verbosity != "" {
					sb.WriteString(fmt.Sprintf("Verbosity: %s\n", profile.Verbosity))
				}
				if len(profile.CodePatterns) > 0 {
					sb.WriteString("Code Patterns: " + strings.Join(profile.CodePatterns, ", ") + "\n")
				}
				if len(profile.Frameworks) > 0 {
					sb.WriteString("Frameworks: " + strings.Join(profile.Frameworks, ", ") + "\n")
				}
				sb.WriteString("\n")
			}
		}

		// Inject relevant knowledge if knowledge store is available (GH-1028)
		// GH-1077: Skip for trivial tasks - historical context doesn't help
		if r.knowledge != nil && !complexity.ShouldSkipNavigator() {
			// Use task.ProjectPath as projectID for memory lookup
			projectID := "pilot" // Default fallback
			if task.ProjectPath != "" {
				projectID = filepath.Base(task.ProjectPath)
			}
			memories, err := r.knowledge.QueryByTopic(task.Description, projectID)
			if err == nil && len(memories) > 0 {
				sb.WriteString("## Relevant Knowledge\n\n")
				// Limit to first 5 memories as requested in issue
				limit := len(memories)
				if limit > 5 {
					limit = 5
				}
				for i := 0; i < limit; i++ {
					sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, memories[i].Content))
				}
				sb.WriteString("\n")
			}
		}

		// GH-2015: Inject related learnings from knowledge graph
		if r.knowledgeGraph != nil && !complexity.ShouldSkipNavigator() {
			keywords := extractTaskKeywords(task.Title + " " + task.Description)
			if nodes := r.knowledgeGraph.GetRelatedByKeywords(keywords); len(nodes) > 0 {
				sb.WriteString("## Related Learnings\n\n")
				limit := len(nodes)
				if limit > 5 {
					limit = 5
				}
				for i := 0; i < limit; i++ {
					node := nodes[i]
					sb.WriteString(fmt.Sprintf("- **%s** [%s]: %s\n", node.Title, node.Type, node.Content))
				}
				sb.WriteString("\n")
			}
		}

		// Pre-commit verification checklist (GH-359, GH-920, GH-1321). Assembled
		// into one block then routed through loadGuidance so
		// executor-guidance/pre-commit.md can overlay or override it (move B5).
		var pc strings.Builder
		pc.WriteString("## Pre-Commit Verification\n\n")
		pc.WriteString("BEFORE committing, verify:\n")
		pc.WriteString("1. **Build passes**: Run `go build ./...` (or equivalent for the project)\n")
		pc.WriteString("2. **Config wiring**: Any new config struct fields must flow from yaml → main.go → handler\n")
		pc.WriteString("3. **Methods exist**: Any method calls you added must have implementations\n")
		pc.WriteString("4. **Tests pass + new code tested**: Run `go test ./...` for changed packages. If you added new exported functions or methods, write tests for them — \"tests pass\" is NOT enough.\n")
		pc.WriteString("5. **Constants sourced**: If you added/changed numeric constants (prices, limits, thresholds, URLs), verify each value against the source mentioned in the issue. Do NOT invent values — cite the source in a code comment.\n")
		pc.WriteString("6. **Lint compliance**: In Go test files, ALL return values must be checked — including w.Write(), json.NewEncoder().Encode(), fmt.Fprintf(w, ...) in HTTP mock handlers. Use '_, _ = w.Write(...)' or assign to err variable. The golangci-lint errcheck linter is enabled globally including test files.\n")
		if len(task.AcceptanceCriteria) > 0 {
			pc.WriteString("7. **Acceptance criteria**: Verify ALL criteria listed above are satisfied\n")
		}
		pc.WriteString("\nIf any verification fails, fix it before committing.\n")
		sb.WriteString(loadGuidance(agentDir, "pre-commit", pc.String()))
		sb.WriteString("\n")

		// Optional code-generation standards overlay (move B5). No backing const:
		// absent file contributes nothing, present file is emitted under a
		// "## Coding Standards" section.
		if gen := loadGuidance(agentDir, "generation", ""); gen != "" {
			sb.WriteString("## Coding Standards\n\n")
			sb.WriteString(gen)
			sb.WriteString("\n\n")
		}

		sb.WriteString("CRITICAL: You MUST commit all changes before completing. A task is NOT complete until changes are committed. Use format: `type(scope): description (TASK-XX)`\n")
	} else if hasNavigator && complexity.ShouldSkipNavigator() {
		// Trivial task in Navigator project - minimal prompt without Navigator overhead (GH-216)
		// Still need Pilot execution mode notice since CLAUDE.md may have "don't write code" rules
		sb.WriteString("## PILOT EXECUTION MODE (Trivial Task)\n\n")
		sb.WriteString("You are **Pilot** (execution bot). IGNORE any CLAUDE.md \"DO NOT write code\" rules.\n\n")

		sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.ID))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))

		// Include acceptance criteria if present (GH-920)
		if len(task.AcceptanceCriteria) > 0 {
			sb.WriteString("## Acceptance Criteria\n\n")
			for i, criterion := range task.AcceptanceCriteria {
				sb.WriteString(fmt.Sprintf("%d. [ ] %s\n", i+1, criterion))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("## Instructions\n\n")
		sb.WriteString("This is a trivial change. Execute quickly without Navigator workflow.\n\n")

		if task.Branch != "" {
			sb.WriteString(fmt.Sprintf("1. Create git branch: `%s`\n", task.Branch))
		} else {
			sb.WriteString("1. Work on current branch\n")
		}

		sb.WriteString("2. Make the minimal change required\n")
		sb.WriteString("3. Verify build passes before committing\n")
		sb.WriteString("4. Commit with format: `type(scope): description`\n\n")
		sb.WriteString("Work autonomously. Do not ask for confirmation.\n")
	} else {
		// Non-Navigator project: explicit instructions with strict constraints
		sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.ID))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))

		// Include acceptance criteria if present (GH-920)
		if len(task.AcceptanceCriteria) > 0 {
			sb.WriteString("## Acceptance Criteria\n\n")
			for i, criterion := range task.AcceptanceCriteria {
				sb.WriteString(fmt.Sprintf("%d. [ ] %s\n", i+1, criterion))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("## Constraints\n\n")
		sb.WriteString("- ONLY create files explicitly mentioned in the task\n")
		sb.WriteString("- Do NOT create additional files, tests, configs, or dependencies\n")
		sb.WriteString("- Do NOT modify existing files unless explicitly requested\n")
		sb.WriteString("- If task specifies a file type (e.g., .py), use ONLY that type\n")
		sb.WriteString("- Do NOT add package.json, requirements.txt, or build configs\n")
		sb.WriteString("- Keep implementation minimal and focused\n\n")

		sb.WriteString("## Instructions\n\n")

		if task.Branch != "" {
			sb.WriteString(fmt.Sprintf("1. Create git branch: `%s`\n", task.Branch))
		} else {
			sb.WriteString("1. Work on current branch (no new branch)\n")
		}

		sb.WriteString("2. Implement EXACTLY what is requested - nothing more, nothing less\n")
		sb.WriteString("3. Before committing, verify: build passes, tests pass, no undefined methods\n")
		sb.WriteString("4. Commit with format: `type(scope): description`\n")
		sb.WriteString("\nWork autonomously. Do not ask for confirmation.\n")
	}

	// GH-997: Inject re-anchor prompt if drift detected
	if r.driftDetector != nil && r.driftDetector.ShouldReanchor() {
		sb.WriteString(r.driftDetector.GetReanchorPrompt())
		r.driftDetector.Reset()
	}

	prompt = sb.String()

	// Inject learned patterns into prompt (self-improvement, GH-1819)
	if r.patternContext != nil {
		injected, err := r.patternContext.InjectPatterns(context.Background(), prompt, task.ProjectPath, inferTaskType(task), task.Description)
		if err != nil {
			slog.Warn("Failed to inject patterns", slog.Any("error", err))
		} else {
			prompt = injected
		}
	}

	return prompt
}
