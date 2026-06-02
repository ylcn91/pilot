#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
port="${PILOT_SMOKE_PORT:-19097}"
tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/pilot-codex-smoke.XXXXXX")"
server_pid=""

cleanup() {
	if [[ -n "${server_pid}" ]] && kill -0 "${server_pid}" 2>/dev/null; then
		kill -INT "${server_pid}" 2>/dev/null || true
		for _ in {1..20}; do
			if ! kill -0 "${server_pid}" 2>/dev/null; then
				break
			fi
			sleep 0.1
		done
		if kill -0 "${server_pid}" 2>/dev/null; then
			kill -TERM "${server_pid}" 2>/dev/null || true
		fi
		for _ in {1..20}; do
			if ! kill -0 "${server_pid}" 2>/dev/null; then
				break
			fi
			sleep 0.1
		done
		if kill -0 "${server_pid}" 2>/dev/null; then
			kill -KILL "${server_pid}" 2>/dev/null || true
		fi
		wait "${server_pid}" 2>/dev/null || true
	fi
	rm -rf "${tmpdir}"
}
trap cleanup EXIT INT TERM

config_path="${tmpdir}/config.yaml"
memory_path="${tmpdir}/memory"
project_path="${tmpdir}/project"
server_log="${tmpdir}/pilot.log"
pilot_bin="${tmpdir}/pilot"
shim_bin="${tmpdir}/bin"
gh_config_dir="${tmpdir}/gh-config"
mkdir -p "${memory_path}" "${project_path}" "${shim_bin}" "${gh_config_dir}"

cat > "${shim_bin}/gh" <<'SH'
#!/usr/bin/env bash
echo "blocked gh invocation during codex runtime smoke: gh $*" >&2
exit 88
SH
chmod +x "${shim_bin}/gh"

cat > "${config_path}" <<YAML
version: "1.0"
gateway:
  host: "127.0.0.1"
  port: ${port}
auth:
  type: "claude-code"
adapters:
  github:
    enabled: false
    token: ""
    repo: ""
    pilot_label: "pilot-smoke-disabled"
    polling:
      enabled: false
      interval: 1h
      label: "pilot-smoke-disabled"
    stale_label_cleanup:
      enabled: false
      interval: 1h
      threshold: 24h
      failed_threshold: 24h
    project_board:
      enabled: false
      source_enabled: false
  linear:
    enabled: false
    polling:
      enabled: false
  slack:
    enabled: false
    socket_mode: false
  telegram:
    enabled: false
    polling: false
  gitlab:
    enabled: false
    polling:
      enabled: false
  azure_devops:
    enabled: false
    polling:
      enabled: false
  jira:
    enabled: false
    polling:
      enabled: false
  asana:
    enabled: false
    polling:
      enabled: false
  plane:
    enabled: false
    polling:
      enabled: false
  discord:
    enabled: false
orchestrator:
  model: "codex-smoke-disabled"
  max_concurrent: 1
  daily_brief:
    enabled: false
  execution:
    mode: "sequential"
    wait_for_merge: false
    poll_interval: 1h
    pr_timeout: 1m
  autopilot:
    enabled: false
    auto_review: false
    auto_merge: false
    auto_create_issues: false
    notify_on_failure: false
    review_feedback:
      enabled: false
memory:
  path: "${memory_path}"
  cross_project: false
  learning:
    enabled: false
    min_confidence: 1
    max_patterns: 0
    include_anti: false
projects:
  - name: "smoke"
    path: "${project_path}"
    navigator: false
    default_branch: "main"
default_project: "smoke"
alerts:
  enabled: false
YAML

if grep -q "qf-studio/pilot" "${config_path}"; then
	echo "smoke config must not reference upstream qf-studio/pilot" >&2
	exit 1
fi

(
	cd "${repo_root}"
	go build -o "${pilot_bin}" ./cmd/pilot
)

(
	cd "${repo_root}"
	env \
		PATH="${shim_bin}:${PATH}" \
		GH_CONFIG_DIR="${gh_config_dir}" \
		GITHUB_TOKEN= \
		GH_TOKEN= \
		GITHUB_APP_ID= \
		GITHUB_APP_PRIVATE_KEY= \
		GITHUB_WEBHOOK_SECRET= \
		GITLAB_TOKEN= \
		AZURE_DEVOPS_PAT= \
		JIRA_API_TOKEN= \
		ASANA_ACCESS_TOKEN= \
		PLANE_API_KEY= \
		DISCORD_BOT_TOKEN= \
		LINEAR_API_KEY= \
		SLACK_BOT_TOKEN= \
		SLACK_APP_TOKEN= \
		TELEGRAM_BOT_TOKEN= \
		"${pilot_bin}" start --config "${config_path}" --project "${project_path}"
) >"${server_log}" 2>&1 &
server_pid="$!"

for _ in {1..80}; do
	if curl -fsS "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
		break
	fi
	if ! kill -0 "${server_pid}" 2>/dev/null; then
		cat "${server_log}" >&2
		exit 1
	fi
	sleep 0.25
done

if ! curl -fsS "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
	cat "${server_log}" >&2
	echo "gateway did not become healthy" >&2
	exit 1
fi

cat > "${tmpdir}/ws-smoke.go" <<'GO'
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type runtimePayload struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Event  *struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
		Error string `json:"error"`
	} `json:"event"`
	Error string `json:"error"`
}

func main() {
	if len(os.Args) != 3 {
		fatalf("usage: ws-smoke <port> <repo-root>")
	}
	port := os.Args[1]
	repoRoot := os.Args[2]

	u := url.URL{Scheme: "ws", Host: "127.0.0.1:" + port, Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	firstPrompt := "Reply with only FIRST."
	secondPrompt := "Reply with only SECOND."
	if err := sendTask(conn, map[string]any{
		"action":  "codexruntime.start",
		"prompt":  firstPrompt,
		"cwd":     repoRoot,
		"sandbox": "read-only",
	}); err != nil {
		fatalf("send first turn: %v", err)
	}
	if err := waitForTurn(conn, "FIRST"); err != nil {
		fatalf("first turn failed: %v", err)
	}

	if err := sendTask(conn, map[string]any{
		"action": "codexruntime.turn",
		"prompt": secondPrompt,
	}); err != nil {
		fatalf("send second turn: %v", err)
	}
	if err := waitForTurn(conn, "SECOND"); err != nil {
		fatalf("second turn failed: %v", err)
	}

	_ = sendTask(conn, map[string]any{"action": "codexruntime.stop"})
	fmt.Println("codex runtime smoke passed")
}

func sendTask(conn *websocket.Conn, payload map[string]any) error {
	return conn.WriteJSON(map[string]any{
		"type":    "task",
		"payload": payload,
	})
}

func waitForTurn(conn *websocket.Conn, want string) error {
	deadline := time.Now().Add(90 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}

	var text strings.Builder
	for time.Now().Before(deadline) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var msg envelope
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		if msg.Type != "progress" {
			continue
		}

		var payload runtimePayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return err
		}
		if payload.Source != "codexruntime" {
			continue
		}
		if payload.Kind == "error" {
			return fmt.Errorf("runtime error: %s", payload.Error)
		}
		if payload.Event == nil {
			continue
		}
		if payload.Event.Error != "" {
			return fmt.Errorf("runtime event error: %s", payload.Event.Error)
		}
		if payload.Event.Type == "agent_message_delta" {
			text.WriteString(payload.Event.Delta)
		}
		if payload.Event.Type == "turn_completed" {
			got := strings.TrimSpace(text.String())
			if !strings.Contains(got, want) {
				return fmt.Errorf("expected %q in response, got %q", want, got)
			}
			return nil
		}
	}
	return fmt.Errorf("timed out waiting for %q", want)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
GO

(
	cd "${repo_root}"
	go run "${tmpdir}/ws-smoke.go" "${port}" "${repo_root}"
)
