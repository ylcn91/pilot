---
name: self-review
kind: atom
mode: overlay
applies_to: executor self-review phase (buildSelfReviewPrompt) — internal/executor/prompt_builder.go
token_budget: ~900 tokens
---
# Self-Review (Tiered)

Executor-side self-review atom for the Pilot ylcn91 fork. Derived from the
lattice `clean-code` atom plus the `review` molecule and the `clean-code-refiner`
/ `review-refiner` severity model. It **overlays** the checklist that the Go
function `buildSelfReviewPrompt` (`internal/executor/prompt_builder.go:475-586`)
emits at runtime: it does not replace those checks, it adds a priority-tiered
discipline on top of them.

This atom runs in `[PILOT-EXEC]` sessions only (see `ExecutorPromptHeader`,
`prompt_builder.go:37`), after staging and before opening the PR.

## Config Resolution

This atom follows the `.agent/executor-guidance/` contract (see `README.md`),
not lattice's `.lattice/config.yaml` — that file is **not** consulted in
executor mode.

1. The embedded default is the checklist `buildSelfReviewPrompt`
   (`prompt_builder.go:475-586`) emits at runtime.
2. `mode: overlay` (this file): the embedded checks apply first, then the tiers
   below are layered on top. Sections are matched by heading; new sections
   (Scope Discipline, Standards Tiering, Priority Tiers) append.
3. If this file is absent, the embedded checks apply alone.

Resolution order: embedded self-review checks → this overlay.

## Self-Validation Checklist (mirrors buildSelfReviewPrompt)

STOP after staging. Run every check. Fix clear failures before the PR; flag
genuine judgment calls (see Ambiguity Signals) instead of silently choosing.

1. **DIFF ANALYSIS** — `git diff --cached`. Look for: methods called that don't
   exist, struct fields added but never used, config fields not wired through,
   imports for unused packages.
2. **BUILD VERIFICATION** — `go build ./...` must pass. If it fails, fix it.
3. **WIRING CHECK** — every NEW struct field is assigned at construction AND
   read somewhere. Search the field name across the codebase.
4. **METHOD EXISTENCE** — every NEW method call has an implementation
   (`func.*methodName`). If missing, implement it.
5. **ISSUE-TO-CHANGES ALIGNMENT** — compare issue title/body with the diff
   (`git diff --name-only HEAD~1`). If the issue names a file ("wire into X",
   "add to Y", "and main.go") that has no changes → emit
   `INCOMPLETE: Issue mentions <file> but it was not modified` and make the
   change.
6. **CONSTANT VALUE SANITY** — for numeric constants (prices, rates, thresholds,
   limits): is the value sourced (URL/reference comment)? Does it match the
   issue's exact values and neighboring magnitudes? If suspicious →
   `SUSPICIOUS_VALUE: <constant> = <value> in <file> — <reason>`. Flag only, do
   not auto-fix uncertain values.
7. **CROSS-FILE PARITY** — if the change touches a file with siblings
   (`backend_*.go`, `adapter_*.go`), verify new error types, enum constants,
   config options, and fallback/retry patterns exist in ALL siblings. If not →
   `PARITY_GAP: <feature> in <file_a> but not <file_b>` and fix it.
8. **LINT CHECK** — `golangci-lint run --new-from-rev=origin/main ./...`. Watch
   for unchecked returns in test mock handlers (`w.Write`, `json.Encode`,
   `SendText`).
9. **ACCEPTANCE CRITERIA** (if present) — verify each criterion MET / UNMET with
   diff evidence. Fix any UNMET before proceeding.
10. **LEARNED PATTERN VALIDATION** (if patternContext present) — check the diff
    against learned anti-patterns. On violation →
    `PATTERN_VIOLATION_FIXED: <pattern> — <fix>`.

**Project-specific checks:** if the loaded custom doc has a Validation Checklist
section, apply it as additional checks after the above.

## Scope Discipline Check

Compare the diff against the task's stated scope. For each symbol (function,
type, field, file) you added or changed that the task did **not** ask for —
cleanup, rename, refactor "while we're here", speculative abstraction, a config
flag for a single caller, defensive code for cases that cannot happen — emit:

```
SCOPE_CREEP: <symbol> — <justify-or-remove>
```

Either justify why it is load-bearing for the task, or remove it before the PR.
Match the change to the request: surgical, not exhaustive. Three similar lines
beat a premature abstraction.

## Standards Tiering

Tag every project-standard violation found above (or by the clean-code anti-
pattern scan) with a tier:

```
STANDARD_VIOLATION: <tier> <rule> — <file:line>
```

`<tier>` is exactly one of:

| tier | meaning | gate |
|---|---|---|
| `blocker` | breaks build/lint/tests, swallowed errors, security/secrets, unwired or dead new code | MUST be fixed before PR |
| `must` | clear clean-code violation: God function, deep nesting, cryptic naming, missing error handling, scope creep kept without justification | MUST be fixed before PR |
| `nice` | stylistic polish, minor naming, optional simplification | optional |

Fix every `blocker` and `must` before opening the PR. `nice` items are optional.

## Priority Tiers (resolution order)

Process findings top-down. Do not advance a tier until the one above is clean.

1. **Tier 0 — blocker.** `go build ./...` fails, lint fails, a test fails, a
   secret leaks, or new code is dead/unwired. The PR cannot open. Fix now.
2. **Tier 1 — must.** Correctness and craft violations:
   `INCOMPLETE:`, `PARITY_GAP:`, unmet acceptance criteria,
   `PATTERN_VIOLATION_FIXED:` targets, and any `STANDARD_VIOLATION: must`. Fix
   before PR.
3. **Tier 2 — nice.** `STANDARD_VIOLATION: nice` and unforced cleanups. Optional;
   leave them if out of scope rather than expanding the diff.

Flag-only signals (`SUSPICIOUS_VALUE:`, unresolved `SCOPE_CREEP:`) are surfaced
in the PR body for the human reviewer; they do not auto-fix.

## Active Anti-Pattern Scan

After the checklist, scan the diff. If found in changed code, fix (or tag with a
`STANDARD_VIOLATION` tier) before the PR.

- [ ] **God Function** — does more than one thing; description needs "and".
- [ ] **Deep Nesting** — 3+ indentation levels → guard clauses / early return.
- [ ] **Cryptic Naming** — `d`, `tmp2`, `processData` → reveal intent.
- [ ] **Swallowed Errors** — empty error path, `_ = err`, generic message.
- [ ] **Dead / Unwired Code** — new field/method/import unused → delete or wire.
- [ ] **Hidden Side Effects** — `getX` also writes/sends → rename or split.
- [ ] **Scope Creep** — change unrelated to the task → `SCOPE_CREEP:` it.
- [ ] **Comments as Deodorant** — comment explaining convoluted code → refactor;
      keep only "why" comments.

## Ambiguity Signals

Multiple valid outcomes — surface both interpretations in the PR body, do not
silently classify.

- **must vs nice tiering**: a near-threshold function with one clear purpose may
  be cleaner whole than split into five smaller functions. Present the tradeoff.
- **Scope creep vs necessary glue**: a small wiring change the task did not name
  but the change cannot work without is not creep. Justify it instead of
  removing.
- **DRY vs premature abstraction**: two identical blocks may diverge later;
  until the third instance with the same reason to change, inlining is valid.
- **SUSPICIOUS_VALUE**: a constant may be correct but unsourced. Flag, never
  auto-fix.

## Final Actions

- Fix everything in **Tier 0** and **Tier 1** (all `blocker` + `must`), commit
  the fix, and re-run build + lint.
- Output `REVIEW_FIXED: <description>` for each issue fixed.
- Output `REVIEW_PASSED` only when every `blocker` and `must` is resolved and
  the build, lint, and acceptance criteria are clean.
- Leave any `SCOPE_CREEP:` and `SUSPICIOUS_VALUE:` flags in the PR body for the
  human reviewer.

## Core Principle

Self-review is the executor's last gate before the PR, not a post-merge review.
It governs the craft and scope of *this diff* — completeness, wiring, parity,
and tier-gated standards compliance — distinct from the architecture and
security atoms (which govern where code lives and its trust boundaries). A
`blocker` or `must` left unfixed is a task failure, the same as a silent no-op.
