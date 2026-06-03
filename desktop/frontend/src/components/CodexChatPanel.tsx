import React, { useEffect, useRef, useState } from 'react'
import { Card } from './ui/Card'
import { Button } from './ui/Button'
import { EmptyState } from './ui/EmptyState'
import { useCodexRuntime } from '../hooks/useCodexRuntime'
import type { RuntimeApprovalRequest, RuntimeSandbox } from '../types'

interface CodexChatPanelProps {
  gatewayURL?: string
  projectPath?: string
}

const SANDBOX_OPTIONS: RuntimeSandbox[] = ['read-only', 'workspace-write', 'danger-full-access']

// running is in-progress → accent (steel); error → rose; idle → sage.
function statusColor(status: string, connected: boolean): string {
  if (!connected) return 'bg-faint'
  if (status === 'running') return 'bg-accent pulse'
  if (status === 'error') return 'bg-danger'
  return 'bg-success'
}

function ApprovalRequest({
  request,
  onChoice,
}: {
  request: RuntimeApprovalRequest
  onChoice: (request: RuntimeApprovalRequest, choice: string) => void
}) {
  return (
    <div className="border border-border rounded-md bg-bg px-3 py-2 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="text-warning truncate">{request.method}</span>
        <span className="text-faint text-meta shrink-0 tabular-nums">#{request.requestId}</span>
      </div>
      <div className="flex flex-wrap gap-1.5 mt-2">
        {request.choices.map((choice) => (
          <Button key={choice} variant="secondary" size="sm" onClick={() => onChoice(request, choice)}>
            {choice}
          </Button>
        ))}
      </div>
    </div>
  )
}

export function CodexChatPanel({ gatewayURL, projectPath }: CodexChatPanelProps) {
  const [prompt, setPrompt] = useState('')
  const [cwd, setCwd] = useState('.')
  const [sandbox, setSandbox] = useState<RuntimeSandbox>('read-only')
  const scrollRef = useRef<HTMLDivElement>(null)
  const cwdEdited = useRef(false)
  const runtime = useCodexRuntime(gatewayURL)

  useEffect(() => {
    if (!cwdEdited.current && projectPath) {
      setCwd(projectPath)
    }
  }, [projectPath])

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [runtime.messages, runtime.reasoning, runtime.approvals])

  function submit() {
    if (runtime.status === 'running') return
    const sent = runtime.sendPrompt({ prompt, cwd, sandbox })
    if (sent) setPrompt('')
  }

  const transcriptEmpty = runtime.messages.length === 0 && runtime.reasoning === ''

  return (
    <Card
      title="Codex"
      className="flex-1 min-h-0"
      action={
        <Button
          variant="secondary"
          size="sm"
          disabled={!runtime.hasSession || runtime.status === 'running'}
          onClick={runtime.resetSession}
        >
          New
        </Button>
      }
    >
      <div className="flex flex-col h-full min-h-0 gap-3">
        <div className="flex items-center gap-2 text-sm shrink-0" role="status">
          <span className={`w-2 h-2 rounded-full shrink-0 ${statusColor(runtime.status, runtime.connected)}`} />
          <span className="text-muted uppercase tracking-wide">{runtime.status}</span>
          {runtime.hasSession && <span className="text-faint">session</span>}
          {runtime.error && <span className="text-danger truncate selectable">{runtime.error}</span>}
        </div>

        <div
          ref={scrollRef}
          role="log"
          aria-live="polite"
          aria-label="Codex transcript"
          className="codex-transcript flex-1 min-h-0 overflow-y-auto log-scroll border border-border rounded-md bg-bg px-3 py-2"
        >
          {transcriptEmpty ? (
            <EmptyState message="No Codex turns yet" />
          ) : (
            <div className="flex flex-col gap-3">
              {runtime.messages.map((message) => (
                <div key={message.id} className="text-sm leading-snug">
                  <div className="text-faint uppercase text-meta tracking-wide">{message.role}</div>
                  <div className="whitespace-pre-wrap text-secondary">{message.text}</div>
                </div>
              ))}
              {runtime.reasoning && (
                <div className="text-sm leading-snug text-muted whitespace-pre-wrap">{runtime.reasoning}</div>
              )}
            </div>
          )}
        </div>

        {runtime.approvals.length > 0 && (
          <div role="alert" className="shrink-0 flex flex-col gap-1.5 max-h-28 overflow-y-auto log-scroll">
            {runtime.approvals.map((request) => (
              <ApprovalRequest
                key={`${request.method}-${request.requestId}`}
                request={request}
                onChoice={runtime.respondToApproval}
              />
            ))}
          </div>
        )}

        <div className="grid grid-cols-[1fr_150px] gap-2 shrink-0">
          <input
            aria-label="Working directory"
            className="bg-bg border border-border rounded-md px-2.5 h-8 text-sm text-secondary outline-none focus:border-accent min-w-0 selectable"
            value={cwd}
            onChange={(event) => {
              cwdEdited.current = true
              setCwd(event.target.value)
            }}
          />
          <select
            aria-label="Sandbox mode"
            className="bg-bg border border-border rounded-md px-2.5 h-8 text-sm text-secondary outline-none focus:border-accent"
            value={sandbox}
            onChange={(event) => setSandbox(event.target.value as RuntimeSandbox)}
          >
            {SANDBOX_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </div>

        <div className="flex gap-2 shrink-0">
          <textarea
            aria-label="Codex prompt"
            placeholder="Ask Codex to make a change…"
            className="flex-1 min-h-[56px] max-h-[96px] resize-none bg-bg border border-border rounded-md px-2.5 py-1.5 text-sm text-secondary outline-none focus:border-accent selectable"
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
          />
          <Button
            variant="primary"
            size="sm"
            className="w-16"
            disabled={!runtime.connected || runtime.status === 'running' || prompt.trim() === ''}
            onClick={submit}
          >
            Run
          </Button>
        </div>
      </div>
    </Card>
  )
}
