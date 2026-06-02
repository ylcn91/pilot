# Pilot — Project Priming (SEED)

> **SEED FILE.** Hand-authored starting point for executor priming. Curated
> facts only; verify against `go.mod`, `CLAUDE.md`, and the code before relying
> on any detail. Refine as the project evolves.

## 1. Architecture

Pilot is an autonomous AI development pipeline: it receives tickets, plans and
executes implementation with Claude Code, opens PRs, and monitors CI. The Go
gateway is the control plane; adapters bridge external systems; the executor
drives Claude Code subprocesses; autopilot closes the loop on CI/merge.

```
Gateway (Go)   → WebSocket control plane + HTTP webhooks
Adapters       → Telegram, GitHub, GitLab, Azure DevOps, Linear, Jira, Slack
Executor       → Claude Code process management + Navigator integration
Autopilot      → CI monitoring, auto-merge, feedback loop, release pipeline
Memory         → SQLite + knowledge graph
Dashboard      → Terminal UI (bubbletea)
```

## 2. Tech Stack (exact versions from go.mod)

- Go `1.24.2`
- CLI: `github.com/spf13/cobra v1.10.2` (+ `spf13/pflag v1.0.10`)
- TUI: `github.com/charmbracelet/bubbletea v1.3.10`, `lipgloss v1.1.0`
- WebSocket: `github.com/gorilla/websocket v1.5.3`
- Desktop shell: `github.com/wailsapp/wails/v2 v2.11.0`
- Cron: `github.com/robfig/cron/v3 v3.0.1`
- SQLite (CGo-free): `modernc.org/sqlite v1.44.3`
- IDs: `github.com/google/uuid v1.6.0`
- Config: `gopkg.in/yaml.v3 v3.0.1`
- `golang.org/x/sys v0.40.0`

## 3. Key Packages (`internal/`)

- `gateway` — WebSocket + HTTP server (control plane, webhooks)
- `adapters` — Telegram, GitHub, GitLab, AzureDevOps, Linear, Jira, Slack
- `executor` — Claude Code runner, prompt building, intent judge
- `autopilot` — CI monitor, auto-merge, release pipeline
- `orchestrator` — task routing / epic coordination
- `memory` — SQLite store + knowledge graph
- `alerts` — alert engine + multi-channel dispatch
- `config` — YAML config loading
- `dashboard` — bubbletea TUI
- `quality`, `intent`, `approval`, `budget`, `health`, `replay`, `wiring`
- `testutil` — fake test tokens (use these, never realistic secrets)
- `text` — untrusted-input sanitization

## 4. Project Structure

```
pilot/
├── cmd/pilot/           # CLI entrypoint
├── cmd/codexruntime-spike/
├── internal/            # all application packages (see §3)
├── docs/                # Nextra v4 documentation site
└── .agent/              # Navigator docs (system/, sops/, tasks/, knowledge/)
```

## 5. Naming

- Conventional Commits, English: `feat:`, `fix:`, `refactor:`, `test:`,
  `docs:`, `chore:`; reference tasks like `feat(gateway): ... TASK-01`.
- Go: standard conventions, `go fmt`, `golangci-lint` (errcheck enabled,
  including test files — always check return values).
- Module path: `github.com/ylcn91/pilot`; import internal packages as
  `github.com/ylcn91/pilot/internal/<pkg>`.
- Tests: table-driven; files end in `_test.go` beside the code.

## 6. Code Example

```go
import "github.com/ylcn91/pilot/internal/testutil"

func TestThing(t *testing.T) {
    token := testutil.FakeSlackBotToken // fake token, never a real pattern
    tests := []struct {
        name string
        in   string
        want string
    }{
        {name: "basic", in: "x", want: "y"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := Do(tt.in); got != tt.want {
                t.Errorf("Do(%q) = %q, want %q", tt.in, got, tt.want)
            }
        })
    }
}
```

## 7. Anti-patterns (avoid)

- Realistic secret patterns in tests — use `internal/testutil` fakes; GitHub
  push protection will block branches otherwise.
- Speculative abstractions / config flags for a single caller (YAGNI).
- `TODO` / `FIXME` markers — do it now or leave nothing.
- New files when an existing one fits; unchecked return values in Go.
- `git checkout` / `git switch` in the repo root — work in a worktree.
- AI attribution in commits or PR bodies.
