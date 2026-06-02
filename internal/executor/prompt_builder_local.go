package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
