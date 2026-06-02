---
name: file-driven executor guidance — .agent/executor-guidance/*.md overlay/override the hardcoded prompt consts
description: loadGuidance(agentDir,key,fallback) lets the team iterate executor standards by editing markdown instead of recompiling Go. Keys: header, evidence-spec, workflow, pre-commit, generation, self-review (reserved).
type: learning
---
The lattice "skills over prompts" idea landed as `.agent/executor-guidance/*.md` + `loadGuidance(agentDir, key, fallback string) string` (`internal/executor/guidance_resolve.go`). It makes the executor's hardcoded Go-const guidance **overridable from markdown without a recompile**.

**How it resolves (per `.agent/executor-guidance/README.md`):**
- No `<key>.md` file → return the hardcoded const VERBATIM (byte-identical fallback; existing tests rely on this).
- `mode: overlay` → const first, then the file body appended.
- `mode: override` → file body replaces the const.
- **`header` and `evidence-spec` are overlay-only** — `override` is forced back to overlay so the `[PILOT-EXEC]` handshake (GH-2328) and the no-op rationale guard can never be deleted.
- Per-file char budget (~8000) with `slog.Warn` on overflow.

**Wired keys:** `header`, `evidence-spec`, `workflow`, `pre-commit` (all overlay fallbacks of real consts), and `generation` (NEW — emits a `## Coding Standards` section only when the file exists). Routed through BOTH `BuildPrompt` and `BuildGuidancePreamble` so every backend (incl. codex-app-server) sees the overlays.

**To add a new guidance key:** drop `.agent/executor-guidance/<key>.md` with `mode:` frontmatter, then `g := loadGuidance(agentDir, "<key>", fallbackConst)` at the prompt-assembly site.

**Reserved/not yet wired:** `self-review` — `buildSelfReviewPrompt(task)` has no `agentDir`; wiring it needs threading `agentDir` into that signature + its `runner.go` caller. The file and key exist; only the call site is missing.

**Pitfall:** the atoms must NOT reference lattice's `.lattice/config.yaml` (it doesn't exist in this repo — the resolution model is const-vs-sibling-file only). Two atoms initially grafted invented `.lattice/config.yaml` keys; corrected before merge.

Related: [[decision_lattice_fowler_integration]], [[pitfall_codex_appserver_bypassed_buildprompt]].
