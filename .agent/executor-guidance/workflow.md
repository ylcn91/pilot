---
name: workflow
kind: atom
mode: overlay
applies_to: pilot-executor sessions (prompts beginning with `[PILOT-EXEC]`)
token_budget: ~2.5k
---

# Autonomous Execution Workflow (atom)

Overlay for the executor-mode workflow that Pilot's `prompt_builder` /
`workflow` constants inject into every child Claude Code session. This atom
is a methodology atom in the spirit of lattice `design-first` and
`context-anchoring`: it does not produce an artifact of its own, it gates
*how* the execution leg moves from task text to a committed, verified change.

Source of truth (do not duplicate, overlay onto):
- `GetAutonomousWorkflowInstructions()` →
  `internal/executor/workflow.go:5` — returns
  `workflowEnforcement + "\n" + autonomousWorkflowInstructions`.
- `workflowEnforcement` → `internal/executor/workflow.go:9` — the mandatory
  `## WORKFLOW CHECK` mode-detection block.
- `autonomousWorkflowInstructions` → `internal/executor/workflow.go:36` —
  the five-phase `## Autonomous Execution Workflow`.
- `ExecutorPromptHeader` → `internal/executor/prompt_builder.go:37` — the
  `[PILOT-EXEC]` sentinel (first line stable per GH-2328).
- `EvidenceBackedSpecDirective` → `internal/executor/prompt_builder.go:52`.
- `buildSelfReviewPrompt(task)` → `internal/executor/prompt_builder.go:475`.

## Config Resolution

1. This atom applies **only** when the prompt begins with `[PILOT-EXEC]`
   (`ExecutorPromptHeader`, `prompt_builder.go:37`). In an interactive dev
   session, defer to the Navigator pipeline in `CLAUDE.md` and stop here.
2. Resolution order: **embedded defaults → repo conventions → this overlay**.
   The embedded defaults are the Go constants above. They are authoritative;
   sections here are matched by heading and replace or extend them.
   - `## WORKFLOW CHECK` and the five phases (`INIT → RESEARCH → IMPL →
     VERIFY → COMPLETE`) overlay `workflowEnforcement` /
     `autonomousWorkflowInstructions` verbatim — do not restate the box, the
     loop triggers, or the `pilot-signal` JSON shapes; honor them as emitted.
   - The `Evidence-Backed Spec` and `Zero-Code Completion` sections below
     are new headings appended to the defaults.
3. Repo conventions consulted (read at INIT, do not invent):
   `.agent/DEVELOPMENT-README.md`, `.agent/sops/`,
   `.agent/system/FEATURE-MATRIX.md`.
4. No `.lattice/config.yaml` is consulted in executor mode; the Go constants
   are the path. Primary language: Go (`go build ./...`, `go test`,
   `golangci-lint run --new-from-rev=origin/main ./...`). Module path is
   `github.com/ylcn91/pilot`.

## Self-Validation Checklist

Run these in order. STOP language: if a check fails, fix before presenting —
do not proceed to COMPLETE.

1. **MODE DECLARED** — Output the `## WORKFLOW CHECK` box first
   (`workflow.go:9`). Loop trigger, Complexity
   (TRIVIAL/SIMPLE/MEDIUM/COMPLEX), Mode (LOOP/TASK/DIRECT). If absent → emit
   before any other output.
2. **RESEARCH BEFORE IMPL** — You found the similar implementation / existing
   pattern before editing (Phase 2, `workflow.go:60`). If you went straight
   to IMPL → stop, research, then resume.
3. **EVIDENCE OVER PRIOR** — If the task cites a file, line, before/after, or
   proof the code is wrong, you applied it even though the code "looked
   correct" (`EvidenceBackedSpecDirective`, `prompt_builder.go:52`). If you
   skipped it on a judgment call → reverse and apply.
4. **NO SILENT NO-OP** — If you genuinely changed nothing, you emitted
   `NO-OP RATIONALE: <file:line> <reason>`. A silent no-op is a task failure
   and the ghost-SHA guard rejects the run (GH-3224). If no rationale and no
   diff → write the rationale or make the change.
5. **BUILD GREEN** — `go build ./...` passes with zero errors (Phase 4,
   `workflow.go:104`). If not → fix before present.
6. **TESTS RUN AND COVER NEW CODE** — `go test ./internal/<changed>/...`
   passes; new exported funcs/methods have matching test cases that appear in
   `-v` output. "All tests pass" with zero new tests is NOT acceptable for
   non-trivial additions (`workflow.go:123`). If not → add tests.
7. **WIRED, NOT ORPHANED** — Each NEW struct field is defined, assigned in
   its constructor/factory, and used; each NEW method call resolves to a real
   `func` of correct signature (`workflow.go:114`, self-review §3/§4). If not
   → wire it.
8. **ISSUE-TO-DIFF ALIGNMENT** — Files named in the task (`wire into X`,
   `add to Y`, `modify Z`) appear in `git diff --name-only HEAD~1`. If a
   named file is missing → `INCOMPLETE: Issue mentions <file> but it was not
   modified` and fix it (self-review §5, GH-652).
9. **COMMIT + EXIT** — Commit as `type(scope): description` (write a specific
   summary, never copy a template), then emit the exit `pilot-signal`
   (`workflow.go:156`). On unrecoverable block after 3 attempts, emit the
   failure exit signal with a concrete `reason`, never loop or give up
   silently (`workflow.go:176`).

Project-specific checks: also run every numbered check in
`buildSelfReviewPrompt` (`prompt_builder.go:475`) — Diff Analysis, Build,
Wiring, Method Existence, Issue-to-Changes Alignment, Constant Value Sanity,
Cross-File Parity, Lint, and (when present) Acceptance Criteria + Learned
Pattern Validation.

## Active Anti-Pattern Scan

After the checklist, search your own output/diff for:

- [ ] **Zero-Code Completion** — declared done with no diff and no
  `NO-OP RATIONALE`.
- [ ] **Prior-Knowledge Override** — skipped an evidence-backed edit because
  the code "looked right" (GH-3222 class).
- [ ] **Phantom Create** — treated a sibling/similarly-named file as proof a
  `CREATE` task was already done, instead of `ls <path>` then creating.
- [ ] **TODO-Instead-Of-Impl** — left a TODO/FIXME where the task expected
  working code (`workflow.go:92`).
- [ ] **Scope Drift** — refactor/rename/style change in files unrelated to
  the task.
- [ ] **Orphaned Field/Method** — added but never assigned, used, or
  implemented.
- [ ] **Suspicious Constant** — unsourced numeric value; flag as
  `SUSPICIOUS_VALUE: <constant> = <value> in <file> — <reason>`, do not
  auto-fix (self-review §6, GH-1321).
- [ ] **Parity Gap** — new error type / enum / fallback added to one sibling
  (`backend_*.go`, `adapter_*.go`) but not the others
  (`PARITY_GAP: ...`, self-review §7).
- [ ] **Missing Mode Box** — execution started without the WORKFLOW CHECK
  block.

## Ambiguity Signals

Genuine judgment calls — surface them, do not resolve silently:

- **Mode selection** — LOOP vs TASK vs DIRECT when complexity sits on a
  boundary (e.g. a SIMPLE change inside a "do all of these" batch). State the
  chosen mode and why in the WORKFLOW CHECK box.
- **Evidence vs no-op** — the task implies a change but reading the code
  suggests it is already satisfied. Default to honoring the spec
  (`EvidenceBackedSpecDirective`); if you still conclude no-op, the
  `NO-OP RATIONALE` marker is mandatory, not optional.
- **DOCUMENT phase scope** — whether a change is "novel enough" to warrant a
  FEATURE-MATRIX row, a `// Decision:` comment, or a new SOP. Trivial
  fixes skip Phase 4.5 (`workflow.go:135`); a new capability does not.
- **Constant sanity** — a value that fits its neighbors but lacks a source
  comment. Flag (`SUSPICIOUS_VALUE`), do not silently change.
- **Test sufficiency** — what counts as "non-trivial" for the new-code-needs-
  tests rule. When unsure, add the test.

## Core Principle

The executor is the **execution leg** of the Navigator + Pilot pipeline
(`ExecutorPromptHeader`): plan elsewhere, ship here. The job is a
real, verified, committed change — never a refusal, a deferral, or a request
to open an issue. Two non-negotiables anchor everything: **research before
implementing** (you must understand the existing pattern before you touch
it — the design-first discipline) and **no zero-code completion** (every run
ends in either a diff that builds and tests green, or an explicit
`NO-OP RATIONALE` — silence is failure). Evidence in the spec outranks the
model's prior knowledge. This atom governs the *flow* (mode → research →
implement → verify → complete) and the *exit contract* (build, tests,
wiring, commit, `pilot-signal`); it defers the line-level marker set
(`INCOMPLETE`, `SUSPICIOUS_VALUE`, `PARITY_GAP`, `REVIEW_FIXED`,
`REVIEW_PASSED`) to `buildSelfReviewPrompt`. Boundary note: this workflow atom
does **not** define the `SCOPE_CREEP` / `STANDARD_VIOLATION` markers — those
are defined by the sibling `self-review.md` atom (overlaying
`buildSelfReviewPrompt`); this atom honors them but does not restate them.
