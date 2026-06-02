import React, { useEffect, useRef, useState } from 'react'
import { Card } from './ui/Card'
import { useCodexRuntime } from '../hooks/useCodexRuntime'
import type { RuntimeApprovalRequest, RuntimeSandbox } from '../types'

interface CodexChatPanelProps {
  gatewayURL?: string
  projectPath?: string
}

const SANDBOX_OPTIONS: RuntimeSandbox[] = ['read-only', 'workspace-write', 'danger-full-access']

function statusColor(status: string, connected: boolean): string {
  if (!connected) return 'bg-gray'
  if (status === 'running') return 'bg-amber pulse'
  if (status === 'error') return 'bg-rose'
  return 'bg-sage'
}

function ApprovalRequest({
  request,
  onChoice,
}: {
  request: RuntimeApprovalRequest
  onChoice: (request: RuntimeApprovalRequest, choice: string) => void
}) {
  return (
    <div className="border border-border bg-bg px-2 py-1.5 text-[10px]">
      <div className="flex items-center justify-between gap-2">
        <span className="text-amber truncate">{request.method}</span>
        <span className="text-gray shrink-0">#{request.requestId}</span>
      </div>
      <div className="flex flex-wrap gap-1 mt-1">
        {request.choices.map((choice) => (
          <button
            key={choice}
            className="px-1.5 py-0.5 border border-border bg-card text-lightgray hover:border-steel hover:text-white"
            type="button"
            onClick={() => onChoice(request, choice)}
          >
            {choice}
          </button>
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

  return (
    <Card title="CODEX" className="flex-[0_0_36%] min-h-[230px]">
      <div className="flex flex-col h-full min-h-0 gap-1.5">
        <div className="flex items-center gap-2 text-[10px] shrink-0">
          <span className={`w-2 h-2 rounded-full ${statusColor(runtime.status, runtime.connected)}`} />
          <span className="text-midgray uppercase">{runtime.status}</span>
          {runtime.hasSession && <span className="text-gray">session</span>}
          {runtime.error && <span className="text-rose truncate">{runtime.error}</span>}
          <button
            className="ml-auto px-1.5 py-0.5 border border-border bg-card text-gray hover:border-steel hover:text-lightgray disabled:hover:border-border disabled:text-slate"
            disabled={!runtime.hasSession || runtime.status === 'running'}
            type="button"
            onClick={runtime.resetSession}
          >
            New
          </button>
        </div>

        <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto log-scroll border border-border bg-bg px-2 py-1.5">
          {runtime.messages.length === 0 && runtime.reasoning === '' ? (
            <div className="text-gray text-[10px]">no codex turns</div>
          ) : (
            <div className="space-y-2">
              {runtime.messages.map((message) => (
                <div key={message.id} className="text-[11px] leading-snug">
                  <div className="text-gray uppercase text-[9px]">{message.role}</div>
                  <div className="whitespace-pre-wrap text-lightgray">{message.text}</div>
                </div>
              ))}
              {runtime.reasoning && (
                <div className="text-[10px] leading-snug text-midgray whitespace-pre-wrap">
                  {runtime.reasoning}
                </div>
              )}
            </div>
          )}
        </div>

        {runtime.approvals.length > 0 && (
          <div className="shrink-0 space-y-1 max-h-24 overflow-y-auto log-scroll">
            {runtime.approvals.map((request) => (
              <ApprovalRequest
                key={`${request.method}-${request.requestId}`}
                request={request}
                onChoice={runtime.respondToApproval}
              />
            ))}
          </div>
        )}

        <div className="grid grid-cols-[1fr_140px] gap-1.5 shrink-0">
          <input
            className="bg-bg border border-border px-2 py-1 text-[11px] text-lightgray outline-none focus:border-steel min-w-0"
            value={cwd}
            onChange={(event) => {
              cwdEdited.current = true
              setCwd(event.target.value)
            }}
          />
          <select
            className="bg-bg border border-border px-2 py-1 text-[11px] text-lightgray outline-none focus:border-steel"
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

        <div className="flex gap-1.5 shrink-0">
          <textarea
            className="flex-1 min-h-[52px] max-h-[88px] resize-none bg-bg border border-border px-2 py-1 text-[11px] text-lightgray outline-none focus:border-steel"
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
          />
          <button
            className="w-16 border border-border bg-card text-lightgray text-[11px] hover:border-steel hover:text-white disabled:text-gray disabled:hover:border-border"
            disabled={!runtime.connected || runtime.status === 'running' || prompt.trim() === ''}
            type="button"
            onClick={submit}
          >
            Run
          </button>
        </div>
      </div>
    </Card>
  )
}
