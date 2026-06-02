---
name: pilot-generation
kind: atom
mode: overlay
applies_to: "Go code generated or modified by Pilot's executor in this repo (internal/**, cmd/pilot/**)"
token_budget: "~1.5 pages; load alongside the executor prompt, not in place of it"
description: "Pilot coding-standards atom. Distilled from the lattice clean-code, architecture, domain-driven-design, secure-coding, and test-quality atoms, adapted to Go and this repository (KISS/DRY/SOLID, table-driven tests, errcheck/govet/staticcheck, no speculative abstractions, validation only at boundaries, conventional commits). Apply during code generation, not as a post-hoc review pass. This overlay refines — does not replace — the executor prompt in internal/executor/prompt_builder.go (ExecutorPromptHeader, EvidenceBackedSpecDirective) and the self-review pass in buildSelfReviewPrompt."
---

# Pilot Generation Standards (Go)

## Config Resolution

This atom is a Pilot-local `mode: overlay`. It does not load `.lattice/config.yaml`.
Resolution order for a Pilot-executor session (`[PILOT-EXEC]` sentinel present):

1. **Defaults** — the lattice code-quality atoms (clean-code, architecture,
   domain-driven-design, secure-coding, test-quality) embedded principles.
2. **Language idioms (Go)** — adapt the defaults to Go as described below; Go
   idioms take precedence over the atoms' pseudocode:
   - *Error Handling* → explicit `if err != nil` returns; wrap with `fmt.Errorf("...: %w", err)`; never discard a returned error (`errcheck`). No panics in library code; panics only in `main` wiring or truly unreachable invariants.
   - *Naming Conventions* → exported `CamelCase`, unexported `camelCase`; no stutter (`executor.Executor`, not `executor.ExecutorExecutor`); receivers short and consistent.
   - *Type System & Object Model* → small structs; accept interfaces, return concrete types; define interfaces at the consumer, not the producer.
   - *Parameter & Function Design* → group >3 related params into a struct (e.g. an options/`Config` value), pass `context.Context` first when present.
   - *Dependency Management* → inject collaborators via struct fields/constructor args for testability; no global singletons for I/O.
3. **Custom overlay** — this document's sections, matched by heading, replace the
   matching default sections; new sections (e.g. *Pilot Executor Alignment*) append.

The resolved standard governs generation. The executor prompt
(`internal/executor/prompt_builder.go`) and `buildSelfReviewPrompt` remain the
operational authority on workflow, build/test gating, and PR creation; this atom
governs the *craft* of the code those phases produce.

## Self-Validation Checklist

STOP after generating each function/struct/file. Verify ALL before moving on.
If a check clearly fails, fix before proceeding. If it is a judgment call
(see Ambiguity Signals), surface options rather than silently deciding.

1. **SINGLE RESPONSIBILITY**: Can you name the function without "and"? If not → extract.
2. **SIZE / COMPLEXITY**: Function focused (~ under 40 lines, cyclomatic ~ under 10)? If not → flatten with guard clauses / early `return`, extract a named helper.
3. **NAMING**: Identifier reveals intent without context? Go-idiomatic case, no stutter? If not → rename.
4. **PARAMETERS**: More than 3 related params → group into a struct. `context.Context` first.
5. **ERROR HANDLING**: Every returned error checked and either handled or wrapped with `%w` and actionable context? No swallowed errors, no bare `_ =` on fallible calls. (`errcheck`)
6. **VALIDATE AT BOUNDARIES ONLY**: Input from a system boundary (HTTP webhook, adapter payload, config file, CLI flag, external API) validated once at entry. Internal callers are trusted — no redundant defensive nil/range checks for framework- or caller-guaranteed invariants.
7. **NO SPECULATIVE ABSTRACTION**: No interface, generic, config flag, or env var introduced for a single caller or a hypothetical future. Three similar lines beat a premature abstraction; wait for the 4th occurrence (with the same reason to change) before extracting.
8. **SCOPE = TASK**: The diff implements the task and nothing else — no drive-by renames, reformatting, or "while we're here" refactors outside the changed code.
9. **SECRETS / LOGGING**: No API key, token, or connection string literal in code; read from config/env. No secret, token, or PII written to logs. Test tokens use `internal/testutil` constants or obviously-fake values.
10. **TESTS FOR NEW CODE**: New behavior has a table-driven test (`tests := []struct{...}{...}` + `for _, tt := range tests { t.Run(tt.name, ...) }`); one behavior per case; uses `t.Helper()` in helpers; no I/O against real network/disk.

**Project-specific checks:** also satisfy the deterministic checks in
`buildSelfReviewPrompt` — Diff Analysis, Build Verification (`go build ./...`),
Wiring Check, Method Existence Check, Issue-to-Changes Alignment, Constant Value
Sanity, Cross-File Parity, and Lint Check
(`golangci-lint run --new-from-rev=origin/main ./...`).

## Active Anti-Pattern Scan

After the checklist, scan the generated Go for these. If found, fix before presenting.

- [ ] **God Function**: long function doing several things; description needs "and" → extract focused functions.
- [ ] **Deep Nesting**: 3+ indentation levels → invert conditions, early `return`/guard clauses.
- [ ] **Swallowed Errors**: `_ = f()` on a fallible call, ignored `err`, or generic message with no context → handle or wrap with `%w`.
- [ ] **Stutter / Cryptic Names**: `executor.ExecutorConfig`, `d`, `tmp2`, `data2` → rename to intent-revealing, idiomatic Go.
- [ ] **Premature Abstraction**: interface/generic/factory introduced for one implementation or one caller → inline until the Rule of Three holds.
- [ ] **Defensive Internal Validation**: nil/bounds checks on values a caller within this repo already guarantees → remove; validate only at boundaries.
- [ ] **Hidden Side Effects**: a `GetX`/getter that also writes state, mutates a field, or emits an event → rename or separate.
- [ ] **Cross-File Parity Gap**: sibling files (e.g. `backend_*.go`, adapter implementations) diverge in error types, config keys, or patterns without reason → align (`PARITY_GAP`).
- [ ] **Magic Constant**: unsourced numeric literal (timeout, limit, retry count) → name it and add a reference/justification comment (`SUSPICIOUS_VALUE`).
- [ ] **Dead Code / Unused Import**: commented-out blocks, unused vars/fields/imports → delete (`govet`, `staticcheck`).
- [ ] **Untestable Logic**: business logic fused with I/O so a unit test needs real network/disk → inject the dependency, extract the pure part.

## Ambiguity Signals

Multiple valid outcomes — present options with reasoning, do not silently choose.

- **Function Size vs Clarity**: a near-threshold function with one clear purpose may read worse split into five tiny helpers. State the tradeoff.
- **DRY vs Premature Abstraction**: two similar blocks may diverge later; until the third instance with the same reason to change, extraction is genuinely ambiguous.
- **Validation Depth**: an internal gateway or adapter behind a trusted boundary may or may not need defense-in-depth re-validation — depends on the deployment/threat model.
- **Interface Now vs Later**: introducing an interface for testability vs accepting a concrete type — justify by an actual second implementation or a real mocking need, not a hypothetical one.
- **NO-OP**: if, after analysis, no code change is genuinely warranted, do not exit silently — emit the `NO-OP RATIONALE: <file:line> <reason>` block required by `EvidenceBackedSpecDirective`.

## Pilot Executor Alignment

This atom sits *inside* the executor pipeline; it does not override it.

- The task spec is evidence-backed: per `EvidenceBackedSpecDirective`, apply an
  explicit, verified change even if the existing code looks correct. These craft
  standards refine *how* you write the change — they are never grounds to refuse,
  defer, or open an upstream issue.
- Respect the `WORKFLOW CHECK` mode (LOOP / TASK / DIRECT) and phase budget from
  `internal/executor/workflow.go`; this atom applies within IMPL and VERIFY.
- Commit with Conventional Commits `type(scope): description` (feat, fix,
  refactor, test, docs, chore), the body explaining *why*. No AI attribution
  trailers.

## Core Principle

Pilot writes Go that a careful maintainer would approve without comment: small,
single-purpose units; idiomatic explicit error handling; validation at the
boundary and trust on the inside; no abstraction ahead of its third user; and a
diff scoped exactly to the task. Clean code (how a unit is written), architecture
(where it lives and which way dependencies point), DDD (rich domain behavior over
anemic data holders), secure-coding (what crosses each trust boundary), and
test-quality (table-driven, one-behavior, isolated) apply during generation —
caught as you write, not retrofitted after `buildSelfReviewPrompt` flags them.
