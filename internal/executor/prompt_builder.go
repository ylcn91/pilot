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

// ExecutorPromptHeader is prepended to every executor-mode prompt so the child
// Claude Code session (and any `CLAUDE.md` in the project) can identify itself
// as a Pilot execution leg, rather than inferring from CWD or prompt-prefix
// heuristics. Keep the first line stable — project `CLAUDE.md` files use it
// with `if prompt begins with [PILOT-EXEC] ...`. GH-2328.
const ExecutorPromptHeader = "[PILOT-EXEC]\n" +
	"You are invoked by Pilot's executor to implement a specific task on this " +
	"repository. You are the execution leg of the Navigator + Pilot pipeline. " +
	"Implement the task described below directly — do not refuse, do not ask " +
	"to create a GitHub issue, do not defer. Write code, run build and tests via " +
	"the stop-gate hook, and complete the task.\n\n"

// EvidenceBackedSpecDirective combats the false-negative no-op (GH-3224):
// the model reads existing code, judges it "looks correct," and exits without
// editing — overriding an explicit, evidence-backed spec with its prior
// knowledge (e.g. GH-3222: "--version" looks like the standard flag, so the
// edit was skipped despite proof the current code was wrong). The executor's
// ghost-SHA guard then rejects the run as "no new commit produced." This
// directive instructs the model to honor the spec over its prior, and to make
// any silent no-op explicit instead.
const EvidenceBackedSpecDirective = "## NON-NEGOTIABLE: implement evidence-backed changes\n\n" +
	"If the task specifies an explicit change (a file, a line, a before/after, or " +
	"evidence that the current code is wrong), you MUST apply it even if the " +
	"existing code looks correct. Your general knowledge does NOT override the " +
	"task's specific, verified claim — the spec was written with proof you may not " +
	"infer from reading the code alone.\n\n" +
	"If the task asks you to CREATE a new file, verify it does not already exist " +
	"(e.g. `ls <path>`), then create it. A sibling or similarly-named existing " +
	"file is a pattern to mirror, NOT evidence the task is done.\n\n" +
	"If, after analysis, you conclude no change is genuinely needed, you MUST emit " +
	"an explicit `NO-OP RATIONALE: <file:line> <reason>` block explaining why, " +
	"rather than exiting silently. A silent no-op is treated as a task failure.\n\n"

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

	// GH-2328: Prepend [PILOT-EXEC] executor-mode header so the child Claude
	// session and any project CLAUDE.md can skip Navigator-only "don't write
	// code" rules explicitly, without relying on CWD or prompt-prefix sniffing.
	sb.WriteString(ExecutorPromptHeader)

	// GH-3224: defeat the false-negative no-op for evidence-backed specs.
	// Injected here so it covers both the Navigator and non-Navigator execution
	// paths below (image/local-mode early returns above are out of scope).
	sb.WriteString(EvidenceBackedSpecDirective)

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

	// Check if project has Navigator initialized (use executionPath for worktree support)
	agentDir := filepath.Join(executionPath, ".agent")
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

		// Embed autonomous workflow instructions (replaces /nav-loop dependency)
		sb.WriteString(GetAutonomousWorkflowInstructions())
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

		// Pre-commit verification checklist (GH-359, GH-920, GH-1321)
		sb.WriteString("## Pre-Commit Verification\n\n")
		sb.WriteString("BEFORE committing, verify:\n")
		sb.WriteString("1. **Build passes**: Run `go build ./...` (or equivalent for the project)\n")
		sb.WriteString("2. **Config wiring**: Any new config struct fields must flow from yaml → main.go → handler\n")
		sb.WriteString("3. **Methods exist**: Any method calls you added must have implementations\n")
		sb.WriteString("4. **Tests pass + new code tested**: Run `go test ./...` for changed packages. If you added new exported functions or methods, write tests for them — \"tests pass\" is NOT enough.\n")
		sb.WriteString("5. **Constants sourced**: If you added/changed numeric constants (prices, limits, thresholds, URLs), verify each value against the source mentioned in the issue. Do NOT invent values — cite the source in a code comment.\n")
		sb.WriteString("6. **Lint compliance**: In Go test files, ALL return values must be checked — including w.Write(), json.NewEncoder().Encode(), fmt.Fprintf(w, ...) in HTTP mock handlers. Use '_, _ = w.Write(...)' or assign to err variable. The golangci-lint errcheck linter is enabled globally including test files.\n")
		if len(task.AcceptanceCriteria) > 0 {
			sb.WriteString("7. **Acceptance criteria**: Verify ALL criteria listed above are satisfied\n")
		}
		sb.WriteString("\nIf any verification fails, fix it before committing.\n\n")

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

// buildLocalModePrompt constructs a problem-solving prompt for local execution (GH-2103).
// It skips Navigator workflow, PR constraints, and project context injection.
// Designed for `pilot task --local` where the goal is direct problem-solving.
// v9: proven baseline from v5m (68.5%).
func readEnvContext(projectPath string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, ".pilot-env-context.txt"))
	if err != nil {
		return ""
	}
	// Cap at 1200 bytes to keep prompt lean
	if len(data) > 1200 {
		data = data[:1200]
	}
	return string(data)
}

func (r *Runner) buildLocalModePrompt(task *Task) (prompt string) {
	defer sanitizePromptReturn("buildLocalModePrompt", &prompt)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Task\n\n%s\n\n", task.Description))

	// Inject pre-discovered environment if available (written by agent.py setup)
	if envCtx := readEnvContext(task.ProjectPath); envCtx != "" {
		sb.WriteString("## Pre-discovered Environment\n\n")
		sb.WriteString("```\n")
		sb.WriteString(envCtx)
		sb.WriteString("```\n\n")
	}

	// Phase 1: Mandatory reconnaissance (ForgeCode: +28pts from enforced planning)
	sb.WriteString("## Phase 1: RECON (mandatory — do ALL before writing ANY code)\n\n")
	sb.WriteString("You MUST complete every step below before writing a single line of solution code.\n\n")
	sb.WriteString("1. **Discover the spec**: read whatever instruction files, README, or example fixtures exist in the workspace. Identify EXACTLY what the task asks you to produce, what format it should take, and what success looks like. Do NOT assume a specific test-file path exists; check the workspace to see what's there.\n")
	sb.WriteString("2. **Inventory the workspace**: list the working directory and read every relevant file. Understand what exists before writing code.\n")
	sb.WriteString("3. **Write a plan**: Create a TODO list with specific steps. State which approach you'll use and why. This is NOT optional.\n\n")

	// Phase 2: Implementation
	sb.WriteString("## Phase 2: IMPLEMENT\n\n")
	sb.WriteString("1. **Start with the simplest working approach** — brute-force beats elegant theory you never finish.\n")
	sb.WriteString("2. **Produce output files EARLY** — partial/placeholder output beats no output.\n")
	sb.WriteString("3. **Verify after EVERY significant change**: run whatever check the workspace provides (test runner, validation script, or comparing your output to the expected fixtures you discovered in RECON).\n")
	sb.WriteString("4. **If tests pass, STOP IMMEDIATELY.** No cleanup, no refactoring, no summary.\n\n")

	// Phase 3: Recovery
	sb.WriteString("## Phase 3: RECOVERY (if tests fail)\n\n")
	sb.WriteString("- If stuck for >10 minutes on one approach: DELETE your code and try a COMPLETELY different algorithm.\n")
	sb.WriteString("- If you've edited the same file 3+ times without test improvement: your approach is wrong. Switch.\n")
	sb.WriteString("- Read test output carefully — often the fix is a format mismatch (wrong filename, wrong precision, missing newline), not a logic error.\n")
	sb.WriteString("- Write analysis scripts instead of reasoning through data manually.\n\n")

	// Environment
	sb.WriteString("## Environment\n\n")
	sb.WriteString("Pre-installed (do NOT reinstall):\n")
	sb.WriteString("- Python: numpy (always available)\n")
	sb.WriteString("- System: git, curl, wget, jq, gcc, g++, make\n")
	sb.WriteString("- Tools: uv, uvx (at /usr/local/bin/)\n")
	sb.WriteString("Many containers also have torch, scipy, pandas, scikit-learn in their Docker image.\n")
	sb.WriteString("**ALWAYS check first**: `python3 -c 'import X'` before installing.\n")
	sb.WriteString("If torch is missing and needed: `pip install --break-system-packages torch --index-url https://download.pytorch.org/whl/cpu`\n")
	sb.WriteString("Container has ~2GB RAM. Do NOT run multiple heavy processes concurrently.\n\n")

	// Rules
	sb.WriteString("## Rules\n\n")
	sb.WriteString("- Work autonomously — never ask for confirmation\n")
	sb.WriteString("- Use `--break-system-packages` with pip\n")
	sb.WriteString("- If a build fails, check error for missing deps — `apt-get install -y <pkg>`\n")
	sb.WriteString("- Keep command timeouts short (30s default) — kill hung processes fast\n")
	sb.WriteString("- Never retry the same failing approach — try something different\n")
	sb.WriteString("- Do NOT spend tokens on explanations or summaries — only code and commands\n")
	sb.WriteString("- If you have written >500 words of reasoning without executing any code, STOP and write code NOW\n")
	sb.WriteString("- Never spend more than 2 tool calls on reading/exploring before writing your first code file\n")

	return sb.String()
}

// buildRetryPrompt constructs a prompt for Claude Code to fix quality gate failures.
// Includes git diff context and explicit strategy-switch instruction to avoid
// repeating the same failed approach.
func (r *Runner) buildRetryPrompt(task *Task, feedback string, attempt int) (prompt string) {
	defer sanitizePromptReturn("buildRetryPrompt", &prompt)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Quality Gate Retry (Attempt %d)\n\n", attempt))
	sb.WriteString("Your PREVIOUS approach FAILED. Do NOT retry the same strategy.\n\n")

	sb.WriteString("## What Failed\n\n")
	sb.WriteString(feedback)
	sb.WriteString("\n\n")

	sb.WriteString("## What You Previously Tried\n\n")
	sb.WriteString("Check `git diff HEAD~1` and `git log --oneline -3` to see your previous changes.\n")
	sb.WriteString("Your previous approach was WRONG. Delete it and start over with a completely different algorithm.\n\n")

	sb.WriteString("## Original Task\n\n")
	sb.WriteString(task.Description)
	sb.WriteString("\n\n")

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Run `git diff HEAD~1` to see what you tried before\n")
	sb.WriteString("2. DELETE your previous approach entirely\n")
	sb.WriteString("3. Implement a COMPLETELY DIFFERENT solution\n")
	sb.WriteString("4. Verify using whatever check the workspace provides (test runner or validation script discovered in RECON).\n")
	sb.WriteString("5. If tests pass, STOP.\n\n")
	sb.WriteString("Work autonomously. Do not ask for confirmation.\n")

	return sb.String()
}

// buildSelfReviewPrompt constructs the prompt for self-review phase.
// The prompt instructs Claude to examine its changes for common issues
// and fix them before PR creation.
func (r *Runner) buildSelfReviewPrompt(task *Task) (prompt string) {
	defer sanitizePromptReturn("buildSelfReviewPrompt", &prompt)
	var sb strings.Builder

	sb.WriteString("## Self-Review Phase\n\n")
	sb.WriteString("Review the changes you just made for completeness. Run these checks:\n\n")

	sb.WriteString("### 1. Diff Analysis\n")
	sb.WriteString("```bash\ngit diff --cached\n```\n")
	sb.WriteString("Examine your staged changes. Look for:\n")
	sb.WriteString("- Methods called that don't exist\n")
	sb.WriteString("- Struct fields added but never used\n")
	sb.WriteString("- Config fields that aren't wired through\n")
	sb.WriteString("- Import statements for unused packages\n\n")

	sb.WriteString("### 2. Build Verification\n")
	sb.WriteString("```bash\ngo build ./...\n```\n")
	sb.WriteString("If build fails, fix the errors.\n\n")

	sb.WriteString("### 3. Wiring Check\n")
	sb.WriteString("For any NEW struct fields you added:\n")
	sb.WriteString("- Search for the field name in the codebase\n")
	sb.WriteString("- Verify the field is assigned when creating the struct\n")
	sb.WriteString("- Verify the field is used somewhere\n\n")

	sb.WriteString("### 4. Method Existence Check\n")
	sb.WriteString("For any NEW method calls you added:\n")
	sb.WriteString("- Search for `func.*methodName` to verify the method exists\n")
	sb.WriteString("- If method doesn't exist, implement it\n\n")

	// GH-652 fix: Check that files mentioned in issue were actually modified
	sb.WriteString("### 5. Issue-to-Changes Alignment Check\n")
	sb.WriteString("Compare the issue title/body with your actual changes:\n\n")
	sb.WriteString("**Issue Title:** " + task.Title + "\n\n")
	if task.Description != "" {
		// Include the full description so the self-review can verify against the
		// complete spec (B2e). Keep only a sane upper bound to avoid a runaway
		// prompt on pathological inputs.
		desc := task.Description
		const maxSelfReviewDescChars = 4000
		if len(desc) > maxSelfReviewDescChars {
			desc = desc[:maxSelfReviewDescChars] + "..."
		}
		sb.WriteString("**Issue Description:** " + desc + "\n\n")
	}
	sb.WriteString("Run:\n")
	sb.WriteString("```bash\ngit diff --name-only HEAD~1\n```\n\n")
	sb.WriteString("Check for MISMATCHES:\n")
	sb.WriteString("- If the issue title mentions specific files (e.g., 'wire X into main.go'), verify those files appear in the diff\n")
	sb.WriteString("- If issue says 'and main.go' but main.go has NO changes, THIS IS INCOMPLETE\n")
	sb.WriteString("- Common patterns: 'wire into X', 'add to Y', 'modify Z' — the named files MUST be modified\n\n")
	sb.WriteString("If files mentioned in the issue are NOT in the diff:\n")
	sb.WriteString("- Output `INCOMPLETE: Issue mentions <file> but it was not modified`\n")
	sb.WriteString("- FIX the issue by making the required changes to those files\n\n")

	// GH-1321: Constant value sanity check
	sb.WriteString("### 6. Constant Value Sanity Check\n")
	sb.WriteString("For any numeric constants in the diff (prices, rates, thresholds, limits):\n")
	sb.WriteString("- Is the value sourced? Look for a comment with URL or reference\n")
	sb.WriteString("- Does it fit the magnitude pattern of neighboring constants in the same block?\n")
	sb.WriteString("- If the issue body specifies exact values, do they match the code EXACTLY?\n\n")
	sb.WriteString("If suspicious: output `SUSPICIOUS_VALUE: <constant> = <value> in <file> — <reason>`\n")
	sb.WriteString("Do NOT auto-fix uncertain values — flag only.\n\n")

	// GH-1321: Cross-file parity check
	sb.WriteString("### 7. Cross-File Parity Check\n")
	sb.WriteString("If your changes touch a file with sibling implementations (e.g., `backend_*.go`, `adapter_*.go`):\n")
	sb.WriteString("1. List siblings: `ls $(dirname <file>)/$(echo <file> | sed 's/_[^_]*//')_*.go`\n")
	sb.WriteString("2. For each sibling, check: does it handle the same error types, config options, and fallback patterns?\n")
	sb.WriteString("3. If you added a new error type or enum constant, verify it exists in ALL sibling files\n")
	sb.WriteString("4. If you added a fallback/retry pattern, check if siblings need the same pattern\n\n")
	sb.WriteString("If parity missing: output `PARITY_GAP: <feature> in <file_a> but not <file_b>` and FIX it.\n\n")

	sb.WriteString("### 8. Lint Check\n")
	sb.WriteString("Run `golangci-lint run --new-from-rev=origin/main ./...` and fix any violations.\n")
	sb.WriteString("Common issue: unchecked return values in test mock handlers (w.Write, json.Encode, SendText).\n\n")

	// GH-1966: Acceptance criteria verification in self-review
	if len(task.AcceptanceCriteria) > 0 {
		sb.WriteString("### 9. Acceptance Criteria Verification\n")
		sb.WriteString("Verify each acceptance criterion against your diff:\n\n")
		for i, criterion := range task.AcceptanceCriteria {
			sb.WriteString(fmt.Sprintf("- [ ] **AC%d**: %s — MET / UNMET (cite diff evidence)\n", i+1, criterion))
		}
		sb.WriteString("\nIf any criterion is UNMET, fix the implementation before proceeding.\n\n")
	}

	// Inject learned patterns for validation (ROAD-02: self-review pattern check)
	if r.patternContext != nil {
		patternBlock, err := r.patternContext.GetPatternsForTask(
			context.Background(), task.ProjectPath, inferTaskType(task), task.Description)
		if err != nil {
			slog.Debug("Failed to get patterns for self-review", slog.Any("error", err))
		} else if patternBlock != "" {
			nextCheck := 9
			if len(task.AcceptanceCriteria) > 0 {
				nextCheck = 10
			}
			sb.WriteString(fmt.Sprintf("### %d. Learned Pattern Validation\n", nextCheck))
			sb.WriteString("Check your changes against these learned patterns from previous executions:\n\n")
			sb.WriteString(patternBlock)
			sb.WriteString("\nFor each anti-pattern listed above, verify your code does NOT violate it.\n")
			sb.WriteString("If you find a violation: FIX it and output `PATTERN_VIOLATION_FIXED: <pattern> — <fix>`\n\n")
		}
	}

	// A3: scope-creep self-critique. New symbols not anchored to the task or
	// acceptance criteria are a frequent source of over-engineering; flag them
	// advisorily so the agent justifies or removes them.
	sb.WriteString("### Scope Discipline Check\n")
	sb.WriteString("Inspect every NEW type, wrapper, or interface in your diff.\n")
	sb.WriteString("For each one whose name is NOT mentioned in the task description or acceptance criteria,\n")
	sb.WriteString("output `SCOPE_CREEP: <symbol> — <justify-or-remove>`.\n")
	sb.WriteString("Prefer removing speculative abstractions over justifying them.\n\n")

	// B7: tiered standards markers. Surface violations with an explicit severity
	// tier so blocker/must items get fixed before the PR. Advisory only — no hard
	// runtime gate is wired here.
	sb.WriteString("### Standards Tiering\n")
	sb.WriteString("Tag any project-standard violation as `STANDARD_VIOLATION: <tier> <rule> — <file:line>`,\n")
	sb.WriteString("where `<tier>` is one of `blocker`, `must`, or `nice`.\n")
	sb.WriteString("Fix every `blocker` and `must` item before opening the PR; `nice` items are optional.\n\n")

	sb.WriteString("### Actions\n")
	sb.WriteString("- If you find issues: FIX them and commit the fix\n")
	sb.WriteString("- Output `REVIEW_FIXED: <description>` if you fixed something\n")
	sb.WriteString("- Output `REVIEW_PASSED` if everything looks good\n\n")

	sb.WriteString("Work autonomously. Fix any issues you find.\n")

	return sb.String()
}

// appendResearchContext adds research findings to the prompt (GH-217).
// Research context is inserted before the task instructions to provide
// codebase context gathered by parallel research subagents.
func (r *Runner) appendResearchContext(prompt string, research *ResearchResult) string {
	if research == nil || len(research.Findings) == 0 {
		return prompt
	}

	var sb strings.Builder

	// Insert research context after the task header but before instructions
	sb.WriteString(prompt)
	sb.WriteString("\n\n")
	sb.WriteString("## Pre-Research Context\n\n")
	sb.WriteString("The following context was gathered by parallel research subagents:\n\n")

	for i, finding := range research.Findings {
		// Limit individual findings to prevent prompt bloat
		trimmed := finding
		if len(trimmed) > 2000 {
			trimmed = trimmed[:2000] + "\n... (truncated)"
		}
		sb.WriteString(fmt.Sprintf("### Research Finding %d\n\n%s\n\n", i+1, trimmed))
	}

	sb.WriteString("Use this context to inform your implementation. Do not repeat the research.\n\n")

	return sb.String()
}

// projectContextBudget caps the whole project-context block injected into the
// executor prompt. Sized to keep priming useful without crowding out the task.
const projectContextBudget = 6000

// loadProjectContext assembles the project-context block injected into Navigator
// prompts. It prefers a curated .agent/system/PRIMING.md (used verbatim); absent
// that, it scrapes key sections from DEVELOPMENT-README.md and appends
// system/ARCHITECTURE.md when present. The result is capped at one overall
// budget, truncated on a heading/line boundary. Signature is stable: callers in
// epic.go depend on loadProjectContext(agentDir) string.
func loadProjectContext(agentDir string) string {
	// (a) Curated priming wins — load it verbatim, no heading slicing.
	if priming, err := os.ReadFile(filepath.Join(agentDir, "system", "PRIMING.md")); err == nil {
		return capProjectContext(strings.TrimSpace(string(priming)))
	}

	// (b) Fall back to scraping DEVELOPMENT-README.md.
	readmePath := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return ""
	}

	text := string(content)
	var sb strings.Builder

	// Extract Key Components table (~500 tokens)
	if components := extractSection(text, "### Key Components", "### "); components != "" {
		sb.WriteString("### Key Components\n\n")
		sb.WriteString(components)
		sb.WriteString("\n\n")
	}

	// Extract Key Files section (~800 tokens)
	if files := extractSection(text, "## Key Files", "## "); files != "" {
		sb.WriteString("## Key Files\n\n")
		sb.WriteString(files)
		sb.WriteString("\n\n")
	}

	// Extract Project Structure (~300 tokens)
	if structure := extractSection(text, "## Project Structure", "## "); structure != "" {
		sb.WriteString("## Project Structure\n\n")
		sb.WriteString(structure)
		sb.WriteString("\n\n")
	}

	// Extract Current Version (~200 tokens) - just the line
	if versionStart := strings.Index(text, "**Current Version:"); versionStart != -1 {
		versionLine := text[versionStart:]
		if newlineIdx := strings.Index(versionLine, "\n"); newlineIdx != -1 {
			versionLine = versionLine[:newlineIdx]
		}
		sb.WriteString(strings.TrimSpace(versionLine))
		sb.WriteString("\n\n")
	}

	// (c) Append ARCHITECTURE.md when present for system-level grounding.
	if arch, err := os.ReadFile(filepath.Join(agentDir, "system", "ARCHITECTURE.md")); err == nil {
		sb.WriteString("## Architecture\n\n")
		sb.WriteString(strings.TrimSpace(string(arch)))
		sb.WriteString("\n\n")
	}

	return capProjectContext(strings.TrimSpace(sb.String()))
}

// capProjectContext enforces projectContextBudget, truncating on a heading/line
// boundary and warning when the block overflows so over-large context is visible.
func capProjectContext(block string) string {
	if len(block) <= projectContextBudget {
		return block
	}

	cut := block[:projectContextBudget]
	// Prefer the last Markdown heading so we don't sever a section mid-body.
	bound := strings.LastIndex(cut, "\n#")
	if bound <= 0 {
		bound = strings.LastIndexByte(cut, '\n')
	}
	if bound > 0 {
		cut = cut[:bound]
	}

	slog.Warn("project_context_truncated",
		slog.String("component", "executor"),
		slog.Int("original_chars", len(block)),
		slog.Int("budget", projectContextBudget),
	)

	return strings.TrimRight(cut, " \n\t") + "\n\n… (project context truncated)"
}

// extractSection extracts content between a start marker and the next occurrence of end marker
func extractSection(text, startMarker, endMarker string) string {
	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return ""
	}

	// Find content after the start marker
	contentStart := startIdx + len(startMarker)
	remaining := text[contentStart:]

	// Find the end boundary - look for next section with same level
	// Use newline + endMarker to ensure we match section headers at line start,
	// not substrings within headers (e.g., "## " within "### ")
	endIdx := len(remaining)
	if endMarker != "" {
		lineMarker := "\n" + endMarker
		if nextIdx := strings.Index(remaining, lineMarker); nextIdx != -1 {
			endIdx = nextIdx
		}
	}

	result := strings.TrimSpace(remaining[:endIdx])

	// Limit to reasonable size to prevent prompt bloat
	if len(result) > 2000 {
		result = result[:2000] + "..."
	}

	return result
}

// findRelevantSOPs scans .agent/sops/ for files matching task keywords
// Returns up to 3 relevant SOP file paths.
func findRelevantSOPs(agentDir string, taskDescription string) []string {
	sopsDir := filepath.Join(agentDir, "sops")
	if _, err := os.Stat(sopsDir); err != nil {
		return nil
	}

	// Extract keywords from task description (simple approach)
	keywords := extractTaskKeywords(taskDescription)
	if len(keywords) == 0 {
		return nil
	}

	var matches []string

	// Walk the sops directory
	err := filepath.Walk(sopsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on error
		}

		// Only check .md files
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".md") {
			filename := strings.ToLower(filepath.Base(path))
			relPath := strings.TrimPrefix(path, agentDir+string(filepath.Separator))

			// Check if any keyword matches the filename
			for _, keyword := range keywords {
				if strings.Contains(filename, strings.ToLower(keyword)) {
					matches = append(matches, relPath)
					break
				}
			}

			// Stop at 3 matches to prevent prompt bloat
			if len(matches) >= 3 {
				return filepath.SkipDir
			}
		}
		return nil
	})

	if err != nil {
		return nil
	}

	return matches
}

// readBoundedExcerpt reads a file and returns at most maxChars of its content,
// truncating on a line boundary and appending an ellipsis when content is cut.
// Deliberately independent of extractSection's 2000-char cap so SOP excerpts
// stay short enough to inline without bloating the prompt.
func readBoundedExcerpt(path string, maxChars int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	s := strings.TrimSpace(string(data))
	if len(s) <= maxChars {
		return s
	}

	cut := s[:maxChars]
	// Prefer a line boundary so we don't slice mid-line.
	if nl := strings.LastIndexByte(cut, '\n'); nl > 0 {
		cut = cut[:nl]
	}
	return strings.TrimRight(cut, " \n\t") + "\n…"
}

// extractTaskKeywords extracts relevant keywords from task description for SOP matching
func extractTaskKeywords(description string) []string {
	// Convert to lowercase for case-insensitive matching
	desc := strings.ToLower(description)

	// Common technical keywords to look for
	keywords := []string{
		"sqlite", "database", "db",
		"telegram", "slack", "github", "gitlab", "jira", "linear",
		"auth", "authentication", "oauth",
		"api", "webhook", "http", "rest", "graphql",
		"test", "testing", "unittest",
		"docker", "kubernetes", "k8s",
		"ci", "cd", "pipeline",
		"alert", "notification", "email",
		"tui", "dashboard", "ui",
		"debug", "debugging", "error",
		"integration", "adapter", "client",
	}

	var found []string
	for _, keyword := range keywords {
		if strings.Contains(desc, keyword) {
			found = append(found, keyword)
		}
	}

	return found
}
