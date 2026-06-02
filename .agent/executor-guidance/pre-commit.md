---
name: pre-commit
kind: atom
mode: overlay
applies_to: pilot-executor sessions implementing a task in the pilot ylcn91 fork (prompts beginning with `[PILOT-EXEC]`, `GitHub Issue #NNN:`, or `Task:`)
token_budget: lightweight guardrail — no standalone budget; consumed inline by the executor prompt builder before the commit step
---

# Pre-Commit Verification Atom

Overlay of the hardcoded `## Pre-Commit Verification` block emitted by
`internal/executor/prompt_builder.go` (the section starting at the
`sb.WriteString("## Pre-Commit Verification\n\n")` call). This atom restates
that gate against the fork's real Makefile targets and the
`internal/testutil` fakes so an executor session can self-check before the
`git commit`.

**Mode: overlay.** The embedded defaults are the hardcoded six-step block in
`prompt_builder.go`. Sections below that share a heading with that block
replace it; new sections (golangci-lint, `gofmt`, `make vet`,
`make check-secrets`, test-token rules) append. This atom does not relax the
hardcoded block — every original step still applies.

## Config Resolution

This atom follows the `.agent/executor-guidance/` contract (see `README.md`);
`.lattice/config.yaml` is **not** consulted in executor mode.

1. The embedded default is the hardcoded `## Pre-Commit Verification` block in
   `prompt_builder.go`.
2. `mode: overlay`: the hardcoded block applies first; the sections below
   restate it against the fork's real Makefile targets and append the extra
   gates (golangci-lint, `gofmt`, `make vet`, `make check-secrets`,
   test-token rules). This atom never relaxes a hardcoded step.
3. If this file is absent, the hardcoded block applies alone — the
   verification commands are repo-grounded, not config-dependent.

## Self-Validation Checklist

Run these in order from the worktree root BEFORE staging or committing. STOP
language: if a step fails, fix it and re-run that step — do not commit on a
red gate.

1. **BUILD** — Run `make build` (`go build $(LDFLAGS) -o bin/pilot
   ./cmd/pilot`). If it fails → fix before commit. This is hardcoded step 1
   ("Build passes: `go build ./...`").
2. **VET** — Run `go vet ./...`. If it reports anything → fix before commit.
3. **LINT** — Run `make lint` (`golangci-lint run`). The `errcheck` linter is
   enabled globally **including test files**: every return value must be
   checked, including `w.Write()`, `json.NewEncoder(w).Encode()`, and
   `fmt.Fprintf(w, ...)` in HTTP mock handlers — assign to `err` or discard
   with `_, _ = w.Write(...)`. If `golangci-lint` is not installed, `make lint`
   prints `golangci-lint not installed, skipping...` and exits 0 — a silent
   pass, NOT real lint coverage; install golangci-lint (or run it directly)
   before relying on this gate. This is hardcoded step 6 ("Lint compliance").
4. **TEST (short)** — Run `make test-short` (`go test -short -race ./...`)
   for the changed packages at minimum. If you added new exported functions
   or methods, write tests for them — "tests pass" is NOT enough. This is
   hardcoded step 4 ("Tests pass + new code tested"). The full
   `make test` (`go test -v -race ./...`) is the stronger gate; run it when
   the change is broad.
5. **CHECK-SECRETS** — Run `make check-secrets`
   (`./scripts/check-secret-patterns.sh`). If it flags a realistic-looking
   token → replace it with a fake (see Test-Token Rules). Never commit a
   value that trips push protection.
6. **GOFMT** — Run `gofmt -l .` (or `make fmt`, which runs `go fmt ./...`
   plus `goimports -w .` when available). If `gofmt -l` prints any path →
   format it before commit. Keep formatting changes scoped to files you
   touched.
7. **CONFIG WIRING** — Any new config struct field must flow yaml →
   `cmd/pilot`/main wiring → handler. An unwired field is a silent no-op.
   This is hardcoded step 2 ("Config wiring").
8. **METHODS EXIST** — Any method call you added must have an implementation.
   This is hardcoded step 3 ("Methods exist").
9. **CONSTANTS SOURCED** — If you added/changed numeric constants (prices,
   limits, thresholds, URLs), verify each value against the source named in
   the issue and cite it in a code comment. Do NOT invent values. This is
   hardcoded step 5 ("Constants sourced").
10. **ACCEPTANCE CRITERIA** — If the task lists acceptance criteria, verify
    ALL are satisfied with diff evidence. This is hardcoded step 7, emitted
    only when criteria are present.

Project-specific checks: the sibling `generation.md` atom in this directory
carries the deeper coding / test-quality / secure-coding standards; apply its
Validation items here as well.

After all ten pass, commit. Hardcoded close-out: a task is NOT complete until
changes are committed. Use the format `type(scope): description (TASK-XX)`.

## Active Anti-Pattern Scan

After the checklist, scan the diff for these before committing:

- [ ] **Realistic secret in a test** — a token matching a real provider shape
      (`xoxb-…`, `sk-…`, `ghp_…`, `AKIA…`). Replace with a `testutil` fake.
- [ ] **Unchecked return in a test handler** — `w.Write(...)`,
      `Encode(...)`, `Fprintf(w, ...)` with a dropped error; `errcheck` will
      block it.
- [ ] **Unwired new config field** — added to a struct but never read in
      `cmd/pilot` or the handler.
- [ ] **New exported symbol with no test** — passing existing tests does not
      cover code you just added.
- [ ] **Unformatted file** — appears in `gofmt -l .` output.
- [ ] **Invented constant** — a numeric/URL literal with no sourcing comment.
- [ ] **`--no-verify` / `--no-gpg-sign`** — never bypass the commit hooks; if
      a hook fails, fix the cause.

## Test-Token Rules

Tests that need tokens or secrets MUST use the obviously-fake constants in
`internal/testutil/tokens.go` (package `testutil`), never realistic patterns
that trip GitHub push protection or `make check-secrets`.

```go
import "github.com/ylcn91/pilot/internal/testutil"

token := testutil.FakeSlackBotToken    // "test-slack-bot-token"
gh    := testutil.FakeGitHubToken      // "test-github-token"
key   := testutil.FakeAnthropicKey     // "test-anthropic-api-key"
```

Available fakes include (non-exhaustive, see `tokens.go` for the full list):
`FakeSlackBotToken`, `FakeSlackAppToken`, `FakeSlackWebhookURL`,
`FakeGitHubToken`, `FakeGitHubPAT`, `FakeOpenAIKey`, `FakeAnthropicKey`,
`FakeLinearAPIKey`, `FakeAWSAccessKeyID`, `FakeAWSSecretKey`, `FakeJWT`,
`FakeBearerToken`, `FakeTelegramBotToken`, `FakeWebhookSecret`,
`FakeJiraAPIToken`, `FakeGitLabToken`, `FakeAzureDevOpsPAT`,
`FakePagerDutyRoutingKey`, `FakeAsanaAccessToken`, `FakePlaneAPIKey`.

If a needed fake is missing, add a new `Fake…` constant to
`internal/testutil/tokens.go` with a `test-…` value rather than inlining a
realistic-looking literal. Realistic shapes (Slack `xoxb-`, OpenAI `sk-`,
GitHub `ghp_`, AWS `AKIA…`) are forbidden even in test files — push
protection has blocked branches for hours over exactly these.

## Ambiguity Signals

Genuine judgment calls — surface them rather than guessing:

- **Short vs full test run**: `make test-short` is the standard gate. If the
  change touches concurrency, race-sensitive paths, or many packages, the
  full `make test` (with `-v -race`) is the safer call. State which you ran.
- **New code needing tests**: a tiny unexported helper folded into an existing
  tested path may not need its own test; a new exported function does. When
  unsure whether coverage is adequate, note it.
- **Constant sourcing**: a value derived from an existing sourced constant
  may not need its own citation; a fresh magic number does.
- **Formatting scope**: `make fmt` can reformat files you did not touch
  (`goimports -w .`). Keep the commit scoped — prefer `gofmt -l` to detect,
  and only format files in your diff.

## Core Principle

The commit is the gate. Everything red must be green first: build, vet, lint,
short tests (with tests for new exported code), secret scan, and formatting —
plus the wiring, method-existence, constant-sourcing, and acceptance-criteria
checks the executor hardcodes. This atom owns the pre-commit verification leg
only; the deeper diff/wiring/parity audit belongs to the Self-Review atom
(`buildSelfReviewPrompt`), and mode detection belongs to the Workflow atom.
Never bypass hooks (`--no-verify`, `--no-gpg-sign`); a failing hook is a
signal to fix, not to skip.
