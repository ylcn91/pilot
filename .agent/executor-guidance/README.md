---
name: executor-guidance
kind: spec
mode: n/a
applies_to: internal/executor/prompt_builder.go, internal/executor/workflow.go
token_budget: 1200 tokens/file (header & evidence-spec ≤ 400)
---

# Executor Guidance Layer — Contract

This directory is the **file-driven guidance layer** for the Pilot executor
prompt. Each `<key>.md` here overlays or overrides a hardcoded Go const in
the WS7 executor. This README is the CONTRACT the future `loadGuidance(key
string) string` function implements — not documentation of existing behavior,
but the spec it must satisfy.

The model is borrowed from **lattice** (`/Users/yalcindoksanbir/lattice`):
guidance as **atoms / molecules / refiners** — skills over prompts,
versioned, composable. There, a `SKILL.md` atom carries embedded defaults and
a custom standards doc in `.lattice/standards/<key>.md` overlays or overrides
them via YAML `mode`. Here the "embedded default" is a Go const string and the
"custom doc" is a sibling `<key>.md`. Same resolution discipline, different
substrate: **prompt strings instead of validation checklists**. Credit the
lattice config-resolution mechanism for the overlay/override semantics below.

## Resolution

For each guidance key, `loadGuidance` resolves the prompt fragment in this
order:

1. **No file present** → return the hardcoded Go const **verbatim**. This is
   the default and must always be safe; a missing or empty `<key>.md` never
   degrades the prompt.
2. **File present, `mode: overlay`** → emit the const **first**, then append
   the file body. The const leads; the file refines, extends, or tightens.
3. **File present, `mode: override`** → the file body **replaces** the const
   entirely. The file must be comprehensive — nothing from the const survives.

Frontmatter is the same single-field shape lattice uses:

```yaml
---
mode: overlay   # or: override
---
```

`mode` is the only required key. If frontmatter is absent or `mode` is
unrecognized, treat it as `overlay` (the conservative default — const is
never silently dropped).

## Keys

Each key maps to one const/function in the WS7 executor. Paths and symbols
below are real; do not invent others.

| Key            | Backing symbol (WS7)                                         | Modes allowed      |
|----------------|-------------------------------------------------------------|--------------------|
| `header`       | `ExecutorPromptHeader` const, `prompt_builder.go:37`        | **overlay only**   |
| `evidence-spec`| `EvidenceBackedSpecDirective` const, `prompt_builder.go:52` | **overlay only**   |
| `workflow`     | `workflowEnforcement` + `autonomousWorkflowInstructions`, `workflow.go:9,36` | overlay / override |
| `self-review`  | `buildSelfReviewPrompt`, `prompt_builder.go:475`            | overlay / override |
| `pre-commit`   | `## Pre-Commit Verification` section, `prompt_builder.go:261` | overlay / override |
| `generation`   | (no backing const yet — code-generation guidance)           | overlay / override |

`generation` has no const today: with no file it contributes nothing, and any
file is emitted as-is. It exists so generation-time guidance can be added by
dropping a file, without a Go change.

## OVERLAY-ONLY keys: `header` and `evidence-spec`

These two keys **must never be overridden**. They are structural, not
stylistic, and `loadGuidance` MUST reject `mode: override` on them (fall back
to overlay, log a warning):

- **`header`** carries the `[PILOT-EXEC]` handshake. Its first line is a
  stable sentinel — project `CLAUDE.md` files branch on
  `if prompt begins with [PILOT-EXEC] …` (GH-2328). Overriding it could drop
  the sentinel and break executor-mode detection downstream.
- **`evidence-spec`** carries the no-op guard: honor the spec over prior
  knowledge, verify-before-create, and the mandatory `NO-OP RATIONALE:`
  marker (GH-3224 / GH-3222). Overriding it could remove the marker contract
  and let silent false-negative no-ops through the ghost-SHA guard.

Overlay on these keys appends *after* the const, so the handshake line and
the no-op guard always ship intact ahead of any local additions.

## Token budget

Per file: **~1200 tokens** soft cap (`header` and `evidence-spec`: **~400**,
since they prepend to every prompt and bloat compounds across the run).
`loadGuidance` should warn past budget, not truncate — truncating an override
mid-section is worse than an oversized prompt. Keep overlays surgical: refine
one section, do not restate the const.

## Authoring rules

- One `<key>.md` per key above; the filename is the key.
- Frontmatter `mode` only; body is plain prompt markdown, English.
- Overlay bodies should match section headings used by the const so a future
  heading-aware merge (lattice-style, replace-by-heading) can tighten the
  append into a true section overlay without a rewrite.
- Keep `header`/`evidence-spec` files (if any) additive only; never restate or
  reword the sentinel line or the `NO-OP RATIONALE:` contract.
