import React from 'react'
import { Card } from './ui/Card'
import { StatusIcon } from './ui/StatusIcon'
import { ProgressBar } from './ui/ProgressBar'
import { Row } from './ui/Row'
import { Skeleton } from './ui/Skeleton'
import { EmptyState } from './ui/EmptyState'
import { Status, STATUS_TEXT_COLOR } from './ui/status'
import { api } from '../provider'

const { OpenInBrowser } = api
import type { QueueTask } from '../types'

const STATUS_ORDER: Record<string, number> = {
  running: 0,
  queued: 1,
  pending: 2,
  failed: 3,
  done: 4,
}

function metaText(task: QueueTask): string {
  switch (task.status) {
    case 'running':
      return `${Math.round(task.progress * 100)}%`
    case 'done':
      return 'done'
    case 'failed':
      return 'fail'
    case 'queued':
      return 'queue'
    case 'pending':
      return 'wait'
    default:
      return ''
  }
}

interface QueueRowProps {
  task: QueueTask
  shimmerIndex: number
}

function QueueRow({ task, shimmerIndex }: QueueRowProps) {
  const status = task.status as Status
  const stateColor = STATUS_TEXT_COLOR[status] ?? 'text-muted'
  const url = task.prURL || task.issueURL

  const destination = task.prURL ? `PR for ${task.issueID}` : task.issueURL ? `issue ${task.issueID}` : ''
  const ariaLabel = url
    ? `Open ${destination}: ${task.title}`
    : `${task.issueID}: ${task.title}`

  return (
    <Row
      onActivate={url ? () => OpenInBrowser(url) : undefined}
      disabled={!url}
      ariaLabel={ariaLabel}
    >
      <StatusIcon status={status} />
      <span className="text-accent text-meta shrink-0 font-semibold tabular-nums whitespace-nowrap">
        {task.issueID}
      </span>
      <span className="text-secondary text-sm flex-1 min-w-0 truncate">
        {task.title}
      </span>
      <div className="shrink-0">
        <ProgressBar
          status={status}
          progress={task.progress}
          shimmerDelay={shimmerIndex}
          className="w-20"
        />
      </div>
      <span className={`text-meta w-9 text-right shrink-0 tabular-nums ${stateColor}`}>
        {metaText(task)}
      </span>
    </Row>
  )
}

interface QueuePanelProps {
  tasks: QueueTask[]
  loaded: boolean
}

// Show running/queued/pending/failed first, then last N done items
const MAX_DONE_VISIBLE = 8

export function QueuePanel({ tasks, loaded }: QueuePanelProps) {
  const sorted = [...tasks].sort((a, b) => {
    const oa = STATUS_ORDER[a.status] ?? 99
    const ob = STATUS_ORDER[b.status] ?? 99
    if (oa !== ob) return oa - ob
    // Within same status, newest first
    return b.id.localeCompare(a.id)
  })

  // Split active vs done
  const active = sorted.filter((t) => t.status !== 'done')
  const done = sorted.filter((t) => t.status === 'done').slice(0, MAX_DONE_VISIBLE)
  const visible = [...active, ...done]
  const hiddenCount = sorted.length - visible.length

  let queuedIdx = 0

  return (
    <Card title={`Queue · ${sorted.length}`} className="flex-1 min-h-0">
      <div className="overflow-y-auto h-full log-scroll -mx-1.5">
        {!loaded ? (
          <Skeleton rows={6} />
        ) : visible.length === 0 ? (
          <EmptyState message="No active tasks" />
        ) : (
          <div className="flex flex-col gap-0.5">
            {visible.map((task) => {
              const shimmerIndex = task.status === 'queued' ? queuedIdx++ : 0
              return <QueueRow key={task.id} task={task} shimmerIndex={shimmerIndex} />
            })}
            {hiddenCount > 0 && (
              <div className="text-faint text-meta px-2.5 py-1.5 text-center">
                +{hiddenCount} more completed
              </div>
            )}
          </div>
        )}
      </div>
    </Card>
  )
}
