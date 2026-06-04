package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

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
	sb.WriteString(fmt.Sprintf("Run `golangci-lint run --new-from-rev=%s ./...` and fix any violations.\n", selfReviewLintBaseRef(task)))
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

func selfReviewLintBaseRef(task *Task) string {
	base := "main"
	if task != nil && strings.TrimSpace(task.BaseBranch) != "" {
		base = strings.TrimSpace(task.BaseBranch)
		base = strings.TrimPrefix(base, "origin/")
		base = strings.TrimPrefix(base, "refs/heads/")
	}
	return "origin/" + base
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
