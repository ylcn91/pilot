import { useCallback, useEffect, useRef, useState } from 'react'
import type {
  CodexChatMessage,
  GatewayMessage,
  RuntimeApprovalRequest,
  RuntimeApprovalResponsePayload,
  RuntimeProgressPayload,
  RuntimeSandbox,
  RuntimeStartPayload,
  RuntimeStopPayload,
  RuntimeTurnPayload,
} from '../types'

type RuntimeStatus = 'disconnected' | 'connected' | 'running' | 'completed' | 'error'

interface SendPromptOptions {
  prompt: string
  cwd?: string
  model?: string
  sandbox?: RuntimeSandbox
}

interface CodexRuntimeState {
  connected: boolean
  status: RuntimeStatus
  hasSession: boolean
  messages: CodexChatMessage[]
  reasoning: string
  approvals: RuntimeApprovalRequest[]
  error?: string
}

const RECONNECT_MS = 2000

function runtimeWSURL(gatewayURL?: string): string {
  if (gatewayURL) {
    const url = new URL(gatewayURL)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    url.pathname = '/ws'
    url.search = ''
    url.hash = ''
    return url.toString()
  }

  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws`
}

function messageID(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function appendAssistantDelta(messages: CodexChatMessage[], delta: string): CodexChatMessage[] {
  if (messages.length === 0 || messages[messages.length - 1].role !== 'assistant') {
    return [...messages, { id: messageID('assistant'), role: 'assistant', text: delta }]
  }

  const next = messages.slice()
  const last = next[next.length - 1]
  next[next.length - 1] = { ...last, text: last.text + delta }
  return next
}

function parseProgressMessage(raw: MessageEvent): RuntimeProgressPayload | null {
  const message = JSON.parse(raw.data) as GatewayMessage<RuntimeProgressPayload>
  if (message.type !== 'progress' || message.payload?.source !== 'codexruntime') {
    return null
  }
  return message.payload
}

export function useCodexRuntime(gatewayURL?: string) {
  const [state, setState] = useState<CodexRuntimeState>({
    connected: false,
    status: 'disconnected',
    hasSession: false,
    messages: [],
    reasoning: '',
    approvals: [],
  })
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const unmounted = useRef(false)

  const send = useCallback((payload: RuntimeStartPayload | RuntimeTurnPayload | RuntimeStopPayload | RuntimeApprovalResponsePayload): boolean => {
    const ws = wsRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      setState((prev) => ({ ...prev, status: 'error', error: 'gateway websocket is not connected' }))
      return false
    }

    ws.send(JSON.stringify({ type: 'task', payload }))
    return true
  }, [])

  const sendPrompt = useCallback((options: SendPromptOptions): boolean => {
    const prompt = options.prompt.trim()
    if (!prompt) return false

    const payload: RuntimeStartPayload | RuntimeTurnPayload = state.hasSession
      ? {
          action: 'codexruntime.turn',
          prompt,
        }
      : {
          action: 'codexruntime.start',
          prompt,
          cwd: options.cwd?.trim() || '.',
          model: options.model?.trim() || undefined,
          sandbox: options.sandbox || 'read-only',
        }

    const sent = send(payload)
    if (!sent) return false

    setState((prev) => ({
      ...prev,
      status: 'running',
      hasSession: true,
      error: undefined,
      reasoning: '',
      approvals: [],
      messages: [...prev.messages, { id: messageID('user'), role: 'user', text: prompt }],
    }))
    return true
  }, [send, state.hasSession])

  const resetSession = useCallback((): boolean => {
    const sent = send({ action: 'codexruntime.stop' })
    setState((prev) => ({
      ...prev,
      status: sent && prev.connected ? 'connected' : prev.status,
      hasSession: false,
      error: undefined,
      reasoning: '',
      approvals: [],
      messages: [],
    }))
    return sent
  }, [send])

  const respondToApproval = useCallback((request: RuntimeApprovalRequest, choice: string): boolean => {
    const isPermission = request.method === 'item/permissions/requestApproval'
    const sent = send({
      action: 'codexruntime.approval.respond',
      requestId: request.requestId,
      decision: isPermission ? undefined : choice,
      scope: isPermission ? choice : undefined,
    })
    if (!sent) return false

    setState((prev) => ({
      ...prev,
      approvals: prev.approvals.filter((item) => item.requestId !== request.requestId),
    }))
    return true
  }, [send])

  useEffect(() => {
    unmounted.current = false

    function clearReconnect() {
      if (reconnectTimer.current) {
        clearTimeout(reconnectTimer.current)
        reconnectTimer.current = null
      }
    }

    function connect() {
      clearReconnect()
      if (unmounted.current) return

      const ws = new WebSocket(runtimeWSURL(gatewayURL))
      wsRef.current = ws

      ws.onopen = () => {
        setState((prev) => ({ ...prev, connected: true, status: prev.status === 'running' ? 'running' : 'connected', error: undefined }))
      }

      ws.onmessage = (event) => {
        try {
          const payload = parseProgressMessage(event)
          if (!payload) return

          setState((prev) => {
            if (payload.kind === 'error') {
              return { ...prev, status: 'error', error: payload.error || 'codex runtime error' }
            }

            if (payload.kind === 'approval_request' && payload.requestId != null && payload.method) {
              return {
                ...prev,
                approvals: [
                  ...prev.approvals,
                  {
                    requestId: payload.requestId,
                    method: payload.method,
                    params: payload.params,
                    choices: payload.choices || [],
                  },
                ],
              }
            }

            const runtimeEvent = payload.event
            if (!runtimeEvent) return prev

            if (runtimeEvent.type === 'agent_message_delta' && runtimeEvent.delta) {
              return {
                ...prev,
                messages: appendAssistantDelta(prev.messages, runtimeEvent.delta),
              }
            }
            if (runtimeEvent.type === 'reasoning_delta' && runtimeEvent.delta) {
              return { ...prev, reasoning: prev.reasoning + runtimeEvent.delta }
            }
            if (runtimeEvent.type === 'turn_started') {
              return { ...prev, status: 'running', hasSession: true, error: undefined }
            }
            if (runtimeEvent.type === 'turn_completed') {
              return { ...prev, status: 'completed' }
            }
            if (runtimeEvent.type === 'error') {
              return { ...prev, status: 'error', error: runtimeEvent.error || 'codex runtime error' }
            }

            return prev
          })
        } catch {
          // Ignore malformed gateway messages.
        }
      }

      ws.onclose = () => {
        wsRef.current = null
        setState((prev) => ({ ...prev, connected: false, status: prev.status === 'running' ? 'running' : 'disconnected' }))
        if (!unmounted.current) {
          reconnectTimer.current = setTimeout(connect, RECONNECT_MS)
        }
      }

      ws.onerror = () => {
        ws.close()
      }
    }

    connect()

    return () => {
      unmounted.current = true
      clearReconnect()
      if (wsRef.current) {
        wsRef.current.onclose = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [gatewayURL])

  return {
    ...state,
    sendPrompt,
    resetSession,
    respondToApproval,
  }
}
