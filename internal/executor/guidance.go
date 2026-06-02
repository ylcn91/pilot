package executor

import (
	"fmt"
	"strings"
)

// BuildGuidancePreamble composes the .agent priming that BuildPrompt injects
// for the Claude Code backend, so the codex-app-server backend (which bypasses
// BuildPrompt) can prepend the same guidance to its first turn. It reuses the
// existing package-level pieces rather than reimplementing them, and returns ""
// only when every piece is empty (e.g. no .agent dir and empty consts).
//
// agentDir is the path to the project's .agent directory; taskDescription is
// the user prompt used to select relevant SOPs.
func BuildGuidancePreamble(agentDir, taskDescription string) string {
	var parts []string

	if h := strings.TrimSpace(ExecutorPromptHeader); h != "" {
		parts = append(parts, h)
	}
	if d := strings.TrimSpace(EvidenceBackedSpecDirective); d != "" {
		parts = append(parts, d)
	}
	if ctx := strings.TrimSpace(loadProjectContext(agentDir)); ctx != "" {
		parts = append(parts, "## Project Context\n\n"+ctx)
	}
	if sops := findRelevantSOPs(agentDir, taskDescription); len(sops) > 0 {
		var sb strings.Builder
		sb.WriteString("## Relevant SOPs\n\n")
		sb.WriteString("Check these before implementing:\n")
		for _, sop := range sops {
			sb.WriteString(fmt.Sprintf("- `.agent/%s`\n", sop))
		}
		parts = append(parts, strings.TrimSpace(sb.String()))
	}
	if wf := strings.TrimSpace(GetAutonomousWorkflowInstructions()); wf != "" {
		parts = append(parts, wf)
	}

	return strings.Join(parts, "\n\n")
}
