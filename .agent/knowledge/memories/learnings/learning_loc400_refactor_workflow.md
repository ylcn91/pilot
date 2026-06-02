---
name: learning_loc400_refactor_workflow
description: Multi-agent worktree workflow that split 208/215 oversized Go files under 400 LOC; remaining 7 god-functions + how to finish
metadata:
  type: learning
---

# LOC < 400 refactor via multi-agent worktree workflows

Goal: every production AND test `.go` file < 400 LOC, behavior-preserving, build never broken.
Branch `refactor` (from `dev`), merged back to `dev`. Baseline before work: `go test ./...`
= 41 ok / 0 fail.

## Result of this session
- 215 violations (85 prod + 130 test) → **208 fixed & merged to dev**, **7 remaining**.
- `dev` build + vet + `go test ./...` all green (41 ok / 0 fail, baseline parity).

## What worked (reusable method)
- **Move-only split**: relocate whole top-level decls into new same-package files; existing
  tests prove parity (no new tests needed for relocation). One commit per source file.
- **Isolation**: one git worktree per unit (`.worktrees/<id>`, branch `rf/<id>` off `refactor`).
  Disjoint file sets across units ⇒ all merges conflict-free (`merge-base`-aware).
- **Tools** in `.agent/analysis/2026-06-02-project-audit/tools/`: `bucket.py` (scan >400 →
  units, `BUCKET_EXCLUDE` to skip god-funcs), `merge-wave.sh` (merge unit branches into
  `refactor`), `verify-units.sh` (ground-truth build/loc/test per worktree — do NOT trust
  agent self-reports).
- Test-file naming rule: new test files MUST end `_test.go` → `<base>_<concern>_test.go`
  (NOT `<base>_test_<concern>.go`, which compiles as production code and breaks the build).

## Critical lesson: API rate limit
Launching ~45 concurrent agents (5 workflows at once) tripped the API: agents returned
"Server is temporarily limited", 0 tokens, no work → ~94 no-op units. **Run ONE workflow at a
time (9 concurrent cap); idempotent prompt (skip files already ≤400).** At 9-concurrent the
no-op rate dropped to ~1/50.

## Remaining 7 files (for next session)
Need **behavior-preserving extract-method** (a single oversized function — NOT move-only),
verify with `go build` + `go vet` + `go test -race` + adversarial diff review:
- `internal/executor/runner.go` (2495) — `executeWithOptions` (~2450 LOC, `goto retrySucceeded`
  + ~30 threaded locals). Approach: private `executeState` struct holding the locals, convert
  phases to `*Runner` methods taking `*executeState`; keep the retry loop/goto and ALL `defer`s
  in the outer function. A partial pass reached 1086 LOC (feasible) but was discarded unverified.
- `cmd/pilot/start_polling.go` (1567) — `runPollingMode` lifecycle wiring; extract per-concern
  builders (runner/memory/approval/autopilot/alerts/budget/poller/telegram/slack/brief/dashboard).
- `internal/executor/backend_claudecode_run.go` (449) — `executeWithFromPR` (433 LOC) method.

Simple **move-only** splits that just never committed (re-run the generic workflow):
- `internal/alerts/channels_test.go` (1055), `internal/alerts/dispatcher_test.go` (545)
- `internal/executor/backend_openai.go` (617), `internal/executor/backend_qwencode.go` (586)

See [[learning_executor_guidance_overlay_layer]] for executor internals context.
