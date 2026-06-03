import React, { useRef, useEffect } from 'react'
import { Card } from './ui/Card'
import { Skeleton } from './ui/Skeleton'
import { EmptyState } from './ui/EmptyState'
import type { LogEntry } from '../types'

export type { LogEntry }

const LEVEL_COLORS: Record<string, string> = {
  info: 'text-secondary',
  warn: 'text-warning',
  error: 'text-danger',
}

/** Extract [GH-XXXX] or [PROJ-NNN] style task ID from component or message */
function extractTaskID(entry: LogEntry): string | null {
  // Check component field first
  if (entry.component) {
    const match = entry.component.match(/^[A-Z]+-\d+$/)
    if (match) return match[0]
  }
  // Check message for [GH-XXXX] prefix
  const msgMatch = entry.message.match(/^\[([A-Z]+-\d+)\]/)
  if (msgMatch) return msgMatch[1]
  return null
}

/** Strip leading [GH-XXXX] from message if we display it separately */
function stripTaskPrefix(message: string): string {
  return message.replace(/^\[[A-Z]+-\d+\]\s*/, '')
}

interface LogsPanelProps {
  entries: LogEntry[]
  loaded: boolean
}

export function LogsPanel({ entries, loaded }: LogsPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [entries])

  return (
    <Card title="Logs" className="flex-1 min-h-0">
      <div
        ref={scrollRef}
        role="log"
        aria-live="polite"
        aria-label="Activity log"
        className="overflow-y-auto h-full log-scroll selectable"
      >
        {!loaded ? (
          <Skeleton rows={8} />
        ) : entries.length === 0 ? (
          <EmptyState message="No log entries" />
        ) : (
          entries.map((e, i) => {
            const taskID = extractTaskID(e)
            const message = taskID ? stripTaskPrefix(e.message) : e.message
            return (
              <div key={i} className="flex gap-3 text-sm leading-snug py-0.5 px-1">
                <span className="text-muted text-meta tabular-nums shrink-0">{e.ts}</span>
                {taskID && (
                  <span className="shrink-0 font-semibold tabular-nums text-accent">[{taskID}]</span>
                )}
                <span className={LEVEL_COLORS[e.level ?? 'info'] ?? 'text-secondary'}>{message}</span>
              </div>
            )
          })
        )}
      </div>
    </Card>
  )
}
