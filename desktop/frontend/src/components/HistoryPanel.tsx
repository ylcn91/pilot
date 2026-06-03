import React from 'react'
import { Card } from './ui/Card'
import { Row } from './ui/Row'
import { Skeleton } from './ui/Skeleton'
import { EmptyState } from './ui/EmptyState'
import { api } from '../provider'

const { OpenInBrowser } = api
import type { HistoryEntry } from '../types'

function timeAgo(dateStr: string): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  const seconds = Math.floor((Date.now() - d.getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

interface HistoryRowProps {
  entry: HistoryEntry
  isSubIssue?: boolean
}

function HistoryRow({ entry, isSubIssue = false }: HistoryRowProps) {
  const isSuccess = entry.status === 'completed'
  const prefix = isSuccess ? '✓' : '✗'
  const prefixColor = isSuccess ? 'text-success' : 'text-danger'
  const indent = isSubIssue ? 'pl-5' : ''
  const url = entry.prURL || ''
  const ariaLabel = url
    ? `Open PR for ${entry.issueID}: ${entry.title}`
    : `${entry.issueID}: ${entry.title}`

  return (
    <Row
      onActivate={url ? () => OpenInBrowser(url) : undefined}
      disabled={!url}
      ariaLabel={ariaLabel}
      className={indent}
    >
      <span aria-hidden="true" className={`text-sm font-semibold w-4 text-center shrink-0 ${prefixColor}`}>
        {prefix}
      </span>
      <span className="font-semibold text-meta tabular-nums shrink-0 whitespace-nowrap text-accent">
        {entry.issueID}
      </span>
      <span className="text-secondary text-sm flex-1 min-w-0 truncate">{entry.title}</span>
      <span className="text-muted text-meta shrink-0 tabular-nums">{timeAgo(entry.completedAt)}</span>
    </Row>
  )
}

interface EpicGroupProps {
  entry: HistoryEntry
}

function EpicGroup({ entry }: EpicGroupProps) {
  const subIssues = entry.subIssues ?? []
  const total = subIssues.length
  const done = subIssues.filter((s) => s.status === 'completed').length
  const allDone = done === total && total > 0
  const prefix = allDone ? '✓' : '~'
  const prefixColor = allDone ? 'text-success' : 'text-accent'

  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-center gap-3 px-2.5 py-1.5 h-8">
        <span aria-hidden="true" className={`text-sm font-semibold w-4 text-center shrink-0 ${prefixColor}`}>
          {prefix}
        </span>
        <span className="font-semibold text-meta tabular-nums shrink-0 whitespace-nowrap text-accent">
          {entry.issueID}
        </span>
        <span className="text-secondary text-sm flex-1 min-w-0 truncate">{entry.title}</span>
        <span className="text-muted text-meta shrink-0 tabular-nums">[{done}/{total}]</span>
      </div>
      {!allDone && subIssues.map((sub) => <HistoryRow key={sub.id} entry={sub} isSubIssue />)}
    </div>
  )
}

interface HistoryPanelProps {
  entries: HistoryEntry[]
  loaded: boolean
}

export function HistoryPanel({ entries, loaded }: HistoryPanelProps) {
  return (
    <Card title="History" className="shrink-0">
      <div className="overflow-y-auto h-full log-scroll -mx-1.5">
        {!loaded ? (
          <Skeleton rows={3} />
        ) : entries.length === 0 ? (
          <EmptyState message="No completed tasks" />
        ) : (
          <div className="flex flex-col gap-0.5">
            {entries.map((entry) =>
              entry.subIssues && entry.subIssues.length > 0 ? (
                <EpicGroup key={entry.id} entry={entry} />
              ) : (
                <HistoryRow key={entry.id} entry={entry} />
              ),
            )}
          </div>
        )}
      </div>
    </Card>
  )
}
