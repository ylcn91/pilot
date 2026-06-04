# Pilot: AI That Ships Your Tickets

**Navigator plans. Pilot executes.**

## Who is reading this file?

This project ships an autonomous executor (Pilot) that runs Claude Code
against this very repo to implement tickets. That means this `CLAUDE.md`
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
   "Execution Modes" below); the upstream "defer everything to a
   GitHub-issue handoff" rule does **not** apply.

When in doubt, look at the incoming prompt: if it hands you a specific
task with file paths and expected outputs, implement it.

## ⚠️ Git & Worktree Discipline (ALL sessions)

Multiple sessions (interactive terminals, Claude Code sessions, and the
Pilot daemon) operate on this repo **concurrently**. Running `git checkout`
in the shared repo root rips the branch out from under every other session
— this has repeatedly left the root on a stranger's PR branch with orphaned
uncommitted changes and a graveyard of stashes.

**Rules:**

- ❌ **NEVER `git checkout <branch>` / `git switch` in the repo root**
  (this fork's root is `/Volumes/doksanbir/repos/pilot`; on other machines
  use whatever path the fork is cloned to). Keep the root pinned to `main`;
  treat it as reference + build-from-main only.
- ✅ **Do all branch work in your own worktree.** Interactive Claude sessions:
  use the worktree flow (sessions land in `.claude/worktrees/<name>`). The
  Pilot daemon already isolates via `pilot-worktree-GH-*` — leave those alone.
- ✅ Base worktrees on `origin/main` (fresh), not on whatever the root
  happens to be pointing at.
- ✅ Commit in your worktree branch; push it; open a PR. Never pile unrelated
  work onto someone else's branch/PR.
- ❌ Do not `git stash pop` blindly in the root — a pathspec-limited stash or
  a worktree is safer when other sessions may have uncommitted work there.
- If you find the root on a non-`main` branch with uncommitted changes you
  did not make, **STOP** — that's another session's work. Don't checkout,
  don't reset, don't commit it. Flag it.

## Execution Modes (ylcn91 fork)

This fork treats three execution paths as first-class. All of them obey the
Git & Worktree Discipline above — branch work happens in a worktree, never
the repo root. The upstream "interactive sessions must defer to a
GitHub-issue handoff" rule does **not** apply here; direct implementation in
a worktree is fine.

| Mode | How to invoke | When |
|------|---------------|------|
| 1. Pilot daemon | `pilot start --github` auto-picks `pilot`-labeled issues, runs in its own `pilot-worktree-GH-*` | Hands-off, queue-driven execution |
| 2. Claude Code direct | `claude code -p "<task>"` in your own worktree | Interactive/direct implementation by a human or agent |
| 3. Codex | `codex` CLI **or** the `codex app-server` runtime, also in a worktree | Codex-backed direct implementation |

You may plan with Navigator if useful, but creating a `pilot`-labeled GitHub
issue and waiting for the daemon is **optional**, not required, in this fork.

### Quick Commands

```bash
# Direct implementation (Claude Code) in a worktree
claude code -p "Add rate limiting to API endpoints"

# Codex direct (CLI or app-server runtime)
codex "Add rate limiting to API endpoints"

# Optional: hand off to the Pilot daemon via a labeled issue
gh issue create --title "Add rate limiting" --label pilot --body "..."
gh issue list --label pilot --state open
gh pr view <number> && gh pr merge <number>
```

## Executor Backends

Pilot's runner can drive several executor backends, selected via
`executor.backend`:

`claude-code` (`claude code -p`), `qwen-code`, `codex-exec`,
`anthropic-api`, `openai-api`, `opencode`, and `codex-app-server`.

The `codex-app-server` backend now receives `.agent` priming through
`BuildGuidancePreamble`, closing the previously documented gap where the
app-server runtime started without Navigator/`.agent` context that the other
backends already got.

**Pilot runs in a separate terminal** (`pilot start --telegram --github`) and auto-picks issues labeled `pilot`.

---

## Memory: Navigator only (auto-memory disabled for this project)

**This project uses Navigator's memory system as the single source of truth for persistent knowledge.** The Claude Code auto-memory system is **deprecated for new writes** in this project. Its path is **machine-local** — Claude Code derives it from the absolute checkout path, so it is `~/.claude/projects/<slugified-fork-path>/memory/` on whatever machine you're on (do **not** assume the upstream `-Users-aleks-petrov-Projects-startups-pilot` slug; that is one contributor's path).

**Rules:**

- ❌ **Do not write to** `~/.claude/projects/.../memory/MEMORY.md` or any file under that directory. Treat the auto-memory `MEMORY.md` index as read-only legacy context.
- ❌ Do not invoke the auto-memory "save a memory" flow described in the user's global instructions (the `user`/`feedback`/`project`/`reference` taxonomy under `~/.claude/projects/...`).
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
- Plans and executes implementation using Claude Code, Codex, or other
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
Executor          → Claude Code process management + Navigator integration
Autopilot         → CI monitoring, auto-merge, feedback loop, release pipeline
Memory            → SQLite + knowledge graph
Dashboard         → Terminal UI (bubbletea)
```

## Project Structure

```
pilot/
├── cmd/pilot/           # CLI entrypoint
├── internal/
│   ├── gateway/         # WebSocket + HTTP server
│   ├── adapters/        # Telegram, GitHub, GitLab, AzureDevOps, Linear, Jira, Slack
│   ├── executor/        # Claude Code runner + intent judge
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
- ❌ No Claude Code mentions in commits
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
