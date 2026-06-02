---
name: lattice + Fowler reduce-friction patterns integrated into the executor (ylcn91 fork)
description: How the 5 Fowler friction patterns + lattice's file-driven standards model were brought into Pilot's executor via disjoint-worktree multi-agent orchestration, merged to dev as a8d8a52.
type: decision
---
On 2026-06-02 the ylcn91 fork adopted the lattice framework + Martin Fowler's "reduce-friction-ai" series (knowledge-priming, context-anchoring, design-first, encoding-team-standards, feedback-flywheel). The work was decomposed into **disjoint-file worktree workstreams** so multiple agents could implement in parallel without collisions, then integrated by hand and merged to `dev`.

**What landed (all on `dev`, build/vet/`go test -short ./...`/check-secrets green):**

1. **Multi-backend priming parity** — `codex-app-server` (gateway `codexruntime_ws.go` + CLI `pilot chat`) bypassed `BuildPrompt`, so `.agent` priming never reached it. Added `executor.BuildGuidancePreamble(agentDir, taskDescription)` (package-level, no `*Runner`) and prepend it on the FIRST turn only. Now `claude -p`, `codex-exec`, and `codex-app-server` all get the same priming. See [[pitfall_codex_appserver_bypassed_buildprompt]].

2. **File-driven standards layer (lattice "skills over prompts")** — `.agent/executor-guidance/*.md` (README + generation/self-review/workflow/pre-commit atoms) overlay/override the hardcoded Go consts via `loadGuidance(agentDir, key, fallback)`. `header`/`evidence-spec` are overlay-only (never replaced — they carry the `[PILOT-EXEC]` handshake + no-op guard). Iterate standards by editing markdown, no Go recompile. See [[learning_executor_guidance_overlay_layer]].

3. **Planner priming** — `buildPlanningPrompt` (epic.go) now gets `loadProjectContext` + `findRelevantSOPs` + labels. Previously the planner decomposed with LESS context than the executor (the biggest hidden gap).

4. **Knowledge priming robustness** — `loadProjectContext` prefers a curated `.agent/system/PRIMING.md` (verbatim), else scrapes DEVELOPMENT-README.md + ARCHITECTURE.md, under one char budget. Bounded SOP excerpts inlined (top 2).

5. **Context anchoring** — `TaskDoc` Run Decision Log (Constraints/DecisionLog/OpenQuestions/KeyFiles/PlannedSteps) + `AppendDecision`/`loadRunDoc`; intent-retry no longer de-anchored (carries AcceptanceCriteria + constraints).

6. **Feedback flywheel** — post-merge reinforcement wired (`controller.reinforceMergedPatterns` → `LearningLoop.RecordMergeOutcome`) with a `recordedReinforcements` idempotency guard; applied-pattern crediting made precise (only injected pattern IDs, bridged via `PatternContext` without changing `InjectPatterns`' signature).

7. **Memory lifecycle** — `KnowledgeStore.SyncToFiles` implemented AND wired onto the 24h maintenance ticker, **flag-gated `memory.sync_to_files`, default false** (Navigator owns `.agent/knowledge/memories/`, so opt-in). Self-review `ExtractionResult.Tier` (SCOPE_CREEP / STANDARD_VIOLATION:<blocker|must|nice>) now influences saved-pattern confidence (blocker 0.9 / must 0.75 / nice 0.5) + `metadata["tier"]`.

8. **Docs** — CLAUDE.md/AGENTS.md rewritten for fork reality (direct-impl, multi-backend); `/nav-loop` doc/code drift fixed (BuildPrompt is in prompt_builder.go, calls `GetAutonomousWorkflowInstructions()` per GH-987).

**Deferred (honest):** B4 planner LOAD-step (no verified seam), B6 `.agent/reviews/review-log.md` (undefined spec), and the `self-review` loadGuidance key (reserved — needs `agentDir` threaded into `buildSelfReviewPrompt` + its runner.go caller). None block the build.

**Why this shape:** Pilot already did the naive form of most patterns (anti-pattern injection, version/structure priming, confidence-decay memory). The value was in closing the silent gaps (app-server bypass, unprimed planner, post-merge reinforcement thrown away) and adding the one genuinely-missing primitive — the file-driven guidance layer — INSIDE `.agent/` rather than a second `.lattice/` root (rejected as duplication). The mandatory 5-level human design gate was rejected too — it breaks the executor's "implement directly" contract; converted to advisory self-critique instead.

Related: [[learning_executor_guidance_overlay_layer]], [[pitfall_codex_appserver_bypassed_buildprompt]], [[learning_pilot_release_and_binary_path]].
