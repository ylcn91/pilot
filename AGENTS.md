# Pilot: AI That Ships Your Tickets

**Navigator plans. Pilot executes.**

> **This fork (ylcn91) has its own rules: direct implementation applies; the Navigator + Pilot GitHub-issue handoff is not required.**

## Who is reading this file?

This project ships an autonomous executor (Pilot) that runs Codex
against this very repo to implement tickets. That means this `AGENTS.md`
is read by two very different kinds of sessions:

1. **Pilot-executor sessions** — spawned by `pilot start` to implement a
   specific GitHub issue. The prompt describes a concrete task and expects
   code changes, a commit, and a PR. **In these sessions, YOU ARE Pilot.
   Implement the task directly.** Signals you're in this mode:
   - Prompt begins with `GitHub Issue #NNN:` or `Task:`
   - No interactive user is following up
   - CWD is inside a pilot worktree or a branch named `pilot/GH-*`
2. **Interactive dev sessions** — a human developer is planning or
   reviewing work on the Pilot project itself. In this fork (ylcn91),
   direct implementation in your own worktree is permitted (see
   "ylcn91 Fork Rules" below); the upstream GitHub-issue handoff is **not**
   required.

When in doubt, look at the incoming prompt: if it hands you a specific
task with file paths and expected outputs, implement it.

## ⚠️ Git & Worktree Discipline (ALL sessions)

Multiple sessions (interactive terminals, Codex sessions, and the
Pilot daemon) operate on this repo **concurrently**. Running `git checkout`
in the shared repo root rips the branch out from under every other session
— this has repeatedly left the root on a stranger's PR branch with orphaned
uncommitted changes and a graveyard of stashes.

**Rules:**

- ❌ **NEVER `git checkout <branch>` / `git switch` in the repo root**
  (this fork's root is `/Volumes/doksanbir/repos/pilot`; on other machines
  use whatever path the fork is cloned to). Keep this fork's root pinned to
  `dev`; treat it as reference + build-from-dev only.
- ✅ **Do all branch work in your own worktree.** Interactive Codex sessions:
  use the worktree flow (sessions land in `.Codex/worktrees/<name>`). The
  Pilot daemon already isolates via `pilot-worktree-GH-*` — leave those alone.
- ✅ Base worktrees on `dev` / `origin/dev` (fresh), not on whatever the root
  happens to be pointing at.
- ✅ Commit in your worktree branch; push it; open a PR. Never pile unrelated
  work onto someone else's branch/PR.
- ❌ Do not `git stash pop` blindly in the root — a pathspec-limited stash or
  a worktree is safer when other sessions may have uncommitted work there.
- If you find the root on a non-`dev` branch with uncommitted changes you
  did not make, **STOP** — that's another session's work. Don't checkout,
  don't reset, don't commit it. Flag it.

## ylcn91 Fork Rules

This fork does **not** require the upstream "plan with Navigator, hand off
via a `pilot`-labeled GitHub issue, never touch code" pipeline. Its rules:

- ✅ **Direct implementation in your own worktree** is permitted — for humans
  and for agents (Claude Code or Codex). Never branch in the repo root; obey
  the Git & Worktree Discipline above.
- ✅ **Own rules and own memory.** This fork keeps its own conventions and
  routes persistent knowledge through Navigator (`.agent/`); auto-memory paths
  are machine-local and not assumed from upstream.
- ✅ **Multi-backend executors.** Tasks can run through any configured backend
  (`claude code -p`, the `codex` CLI, or the `codex app-server` runtime), not
  Claude Code alone.
- ✅ Handing a task to the Pilot daemon via a `pilot`-labeled issue is still
  available, but it's **one option**, not the mandatory path.

```bash
# Direct implementation in a worktree (Claude Code or Codex)
claude code -p "Add rate limiting to API endpoints"
codex "Add rate limiting to API endpoints"

# Optional: hand off to the Pilot daemon
gh issue create --title "Add rate limiting" --label pilot --body "..."
gh issue list --label pilot --state open
gh pr view <number> && gh pr merge <number>
```

**Pilot runs in a separate terminal** (`pilot start --telegram --github`) and auto-picks issues labeled `pilot`.

---

## Memory: Navigator only (auto-memory disabled for this project)

**This project uses Navigator's memory system as the single source of truth for persistent knowledge.** Any agent auto-memory system is **deprecated for new writes** in this project. Such paths are **machine-local** — derived from the absolute checkout path, so they live under `~/.<agent>/projects/<slugified-fork-path>/memory/` on whatever machine you're on (do **not** assume the upstream `-Users-aleks-petrov-Projects-startups-pilot` slug; that is one contributor's path).

**Rules:**

- ❌ **Do not write to** `~/.Codex/projects/.../memory/MEMORY.md` or any file under that directory. Treat the auto-memory `MEMORY.md` index as read-only legacy context.
- ❌ Do not invoke the auto-memory "save a memory" flow described in the user's global instructions (the `user`/`feedback`/`project`/`reference` taxonomy under `~/.Codex/projects/...`).
- ✅ **Write all new memory to Navigator:**
  - **Experiential knowledge** (patterns, pitfalls, decisions, learnings) → `.agent/knowledge/memories/{type}s/{slug}.md`, with the entry indexed in `.agent/knowledge/graph.json`. The four types are `pattern`, `pitfall`, `decision`, `learning` (see `nav-graph` skill / `memory_writer.py` for the file template).
  - **Architecture / long-lived system docs** → `.agent/system/{topic}.md`
  - **Operational procedures** → `.agent/sops/{category}/{slug}.md`
  - **Active task plans** → `.agent/tasks/TASK-{N}-{slug}.md`; archive to `.agent/tasks/archive/` when done
  - **Session checkpoints** → `.agent/.context-markers/{date}_{slug}.md`
- ✅ **Read both** during sessions. The legacy auto-memory `MEMORY.md` index is still loaded into context automatically and contains months of accumulated nuance — use it as you would Navigator memory, but route any *new* entry to Navigator.
- ✅ When you would have updated a legacy auto-memory file, instead:
  1. Find the equivalent Navigator location (or create one),
  2. Write the new entry there,
  3. Optionally leave a `→ moved to .agent/knowledge/memories/...` pointer in the old file so future searches don't miss the update.
- ✅ Use `nav-graph` skill for graph queries (`/nav-graph "what do we know about X?"`) rather than grepping the auto-memory directory.

**Migration status:** legacy auto-memory contents are being ported into Navigator. Until that migration finishes, treat the auto-memory directory as a frozen archive — readable, not writable.

---

## Project Overview

Pilot is an autonomous AI development pipeline that:
- Receives tickets from GitHub issues, and optionally from Linear, Jira,
  Asana, GitLab, and other adapters (GitHub is the primary source in this fork;
  the others are supported, not authoritative)
- Plans and executes implementation using Codex, Claude Code, or other
  configured executor backends
- Creates PRs and notifies via Slack
- Learns patterns across projects

## Quick Start

```bash
# Build
make build

# Run
./bin/pilot start

# Or development mode
make dev
```

## Architecture

```
Gateway (Go)      → WebSocket control plane + HTTP webhooks
Adapters          → Telegram, GitHub, GitLab, Azure DevOps, Linear, Jira, Slack
Executor          → Codex process management + Navigator integration
Autopilot         → CI monitoring, auto-merge, feedback loop, release pipeline
Memory            → SQLite + knowledge graph
Dashboard         → Terminal UI (bubbletea)
```

### Multi-Backend Executor Support

The executor is backend-agnostic. A task can run through any configured
backend, selected via `executor.backend`:

- `claude code -p` — Claude Code, headless prompt mode
- `codex` CLI — Codex headless execution
- `codex app-server` — the Codex app-server runtime (receives `.agent`
  priming via `BuildGuidancePreamble`, closing the prior gap where the
  app-server started without Navigator/`.agent` context)

Other registered backends: `qwen-code`, `codex-exec`, `anthropic-api`,
`openai-api`, `opencode`.

### Pilot-Managed Executors

Backends are managed by the runner, not invoked ad hoc: Pilot owns process
lifecycle, worktree isolation, prompt building (Navigator/`.agent` priming),
stream parsing, and quality gates uniformly across every backend. Switching
backends does not change the surrounding execution contract.

## Project Structure

```
pilot/
├── cmd/pilot/           # CLI entrypoint
├── internal/
│   ├── gateway/         # WebSocket + HTTP server
│   ├── adapters/        # Telegram, GitHub, GitLab, AzureDevOps, Linear, Jira, Slack
│   ├── executor/        # Codex runner + intent judge
│   ├── autopilot/       # CI monitor, auto-merge, release pipeline
│   ├── alerts/          # Alert engine + multi-channel dispatch
│   ├── memory/          # SQLite + knowledge graph
│   ├── config/          # YAML config
│   ├── dashboard/       # TUI (bubbletea)
│   └── testutil/        # Safe test token constants
├── docs/                # Nextra v4 documentation site
└── .agent/              # Navigator docs
```

## Code Standards

- **Go**: Follow standard Go conventions, `go fmt`, `golangci-lint`
- **Python**: PEP 8, type hints, dataclasses
- **Architecture**: KISS, DRY, SOLID
- **Testing**: Table-driven tests for Go

## Test Token Guidelines

When writing tests that need API tokens or secrets:

- ❌ **DON'T** use realistic patterns that trigger GitHub push protection:
  - Slack bot token-shaped placeholders
  - OpenAI API key-shaped placeholders
  - GitHub PAT-shaped placeholders
  - AWS access key ID-shaped placeholders

- ✅ **DO** use obviously fake tokens:
  - `test-slack-bot-token`
  - `fake-api-key`
  - `test-github-token`

- ✅ **DO** use constants from `internal/testutil/tokens.go`:
  ```go
  import "github.com/ylcn91/pilot/internal/testutil"

  token := testutil.FakeSlackBotToken
  ```

**Why?** GitHub's push protection blocks realistic-looking secrets even in test files. 9 branches were blocked for hours due to this.

## Key Commands

```bash
make build          # Build binary
make dev            # Run in dev mode
make test           # Run tests
make lint           # Run linter
make fmt            # Format code
make install-hooks  # Install git pre-commit hooks
make check-secrets  # Check for secret patterns in tests
```

## Configuration

Config file: `~/.pilot/config.yaml`

Key per-adapter env vars (only what each adapter needs):
- `GITHUB_TOKEN` — GitHub polling + PR creation
- `LINEAR_API_KEY` — Linear webhook adapter
- `SLACK_BOT_TOKEN` — Slack Socket Mode adapter
- `TELEGRAM_BOT_TOKEN` — Telegram adapter

Full reference: `configs/pilot.example.yaml`

## Commit Guidelines

- Format: `type(scope): description`
- Types: feat, fix, refactor, test, docs, chore
- Reference tasks: `feat(gateway): add webhook handler TASK-01`

## Navigator Integration

This project uses Navigator for planning, Pilot for execution:

```bash
/nav-start              # Start session, load context
/nav-task "feature"     # Plan implementation
gh issue create ...     # Hand off to Pilot
```

Documentation in `.agent/`:
- `DEVELOPMENT-README.md` - Navigator index
- `tasks/` - Implementation plans
- `system/` - Architecture docs

## Forbidden Actions

- ❌ No secrets in code
- ❌ No package.json modifications without approval
- ❌ No bulk doc loading (use Navigator lazy loading)
- ❌ No Codex mentions in commits
- ❌ No `git checkout`/`git switch` in the repo root — work in a worktree (see "Git & Worktree Discipline")

## Development Workflow

1. Start Navigator: `/nav-start`
2. Plan feature: `/nav-task "description"`
3. Create issue: `gh issue create --title "..." --label pilot --body "..."`
4. Wait for Pilot to execute and create PR
5. Review PR: `gh pr view <n>`
6. Merge when ready: `gh pr merge <n>`

## Current Status

See `docs/lib/version.ts` for the current release and `.agent/DEVELOPMENT-README.md` § "Current State" for recent feature history.

## Documentation Maintenance

Keep `.agent/*.md` files lean. Alternative locations for long-lived content:

- **Changelog / release notes** → `git log` or GitHub Releases
- **Architectural decisions** → `.agent/system/` (one file per decision)
- **Completed task history** → `.agent/tasks/archive/`
- **Active task plans** → `.agent/tasks/` (remove when merged)

Rules:

- Do **not** append to `## Recent` blocks — replace the block content instead.
- Do **not** let any `.agent/*.md` section grow append-only; prune or archive instead.

<!-- GitHub integration verified -->
