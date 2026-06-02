---
name: codex-app-server bypassed BuildPrompt — no .agent priming reached it
description: The codex-app-server path (gateway WS + `pilot chat`) passed the raw prompt straight into TurnStart, so [PILOT-EXEC] header, project context, SOPs, and workflow instructions never reached it. claude -p and codex-exec were fine.
type: pitfall
---
There are **two prompt paths** in the executor, and they diverged. The subprocess/API backends (`claude-code`/`-p`, `codex-exec`, `qwen-code`, `anthropic-api`, `openai-api`, `opencode`) all consume `runner.BuildPrompt()` output, so they get the full `.agent` priming. But the **codex-app-server** path does NOT:

- Gateway: `internal/gateway/codexruntime_ws.go` passed `task.Prompt` raw into `TextUserInput`.
- CLI: `cmd/pilot/chat.go` passed `opts.prompt` raw into `TextUserInput`.

Neither imports `executor` or calls `BuildPrompt`. So a user driving Pilot through the codex app-server (desktop/web/`pilot chat`) got **none** of: the `[PILOT-EXEC]` header, `EvidenceBackedSpecDirective`, project context, SOP hints, learned-pattern injection, or the autonomous workflow instructions.

**Root cause:** `BuildPrompt` is a `*Runner` method; the gateway and `pilot chat` have no `Runner`. But its building blocks (`loadProjectContext`, `findRelevantSOPs`, `GetAutonomousWorkflowInstructions`, the `ExecutorPromptHeader`/`EvidenceBackedSpecDirective` consts) are **package-level**, so the fix was small.

**Fix (2026-06-02, on `dev`):** added `executor.BuildGuidancePreamble(agentDir, taskDescription) string` (pure, no `*Runner`) and prepend it on the FIRST turn only at both seams (gated by `CodexRuntimeConfig.DisablePriming` / `pilot chat --no-priming`). Follow-up turns are left raw so the preamble isn't re-injected every turn.

**Why it matters:** "usable via claude -p / codex / codex app-server" is only true if all three reach the same priming. When adding a NEW backend or entry surface, check whether it routes through `BuildPrompt`; if not, route it through `BuildGuidancePreamble`.

Related: [[decision_lattice_fowler_integration]], [[learning_executor_guidance_overlay_layer]].
