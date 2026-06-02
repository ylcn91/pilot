# Codex App-Server Runtime

This directory is the staging area for Pilot's interactive Codex runtime.
The runtime is separate from `internal/executor`:

- `codex-exec` is the headless ticket backend that fits `Backend.Execute`.
- `codex app-server` is a long-lived interactive JSON-RPC process for free-form chat, review, patch approval, and desktop/web control.

## A0 spike findings

Verified against `codex-cli 0.136.0-alpha.2`.

- Transport: `codex app-server --stdio` uses newline-delimited JSON messages.
- JSON-RPC envelope: requests are `{ "id": 1, "method": "...", "params": ... }`; no `jsonrpc` field is required.
- Initialization: call `initialize`, then send client notification `{ "method": "initialized" }`.
- Thread creation: `thread/start` accepts `cwd`, `approvalPolicy`, `sandbox`, `ephemeral`, `threadSource`, and `sessionStartSource`.
- Turn input: `turn/start.input` is an array of typed user input objects. Text input shape is `{ "type": "text", "text": "...", "text_elements": [] }`.
- Streaming: assistant text arrives as `item/agentMessage/delta`; a complete turn is signaled by `turn/completed`.

Run the spike:

```bash
go run ./cmd/codexruntime-spike --cwd . --prompt "Reply with exactly PONG. Do not run commands."
```

## A1 client boundary

`Client` owns only the local process lifecycle, NDJSON framing, request id correlation, server notification delivery, and stderr draining. It deliberately does not map app-server notifications into Pilot gateway events yet; that belongs in the next slice once the typed methods are stable.

## A2 method boundary

The typed method layer covers the first interactive flow only: `initialize`, `thread/start`, `turn/start`, `turn/steer`, and `turn/interrupt`. The structs are intentionally smaller than the generated schema and include only fields Pilot needs before gateway event mapping.

## A3 event boundary

`MapNotification` converts app-server notifications into stable runtime events while preserving raw params for fields not modeled yet. It is still transport-local; gateway fan-out, desktop state, and approval callbacks remain separate slices.
