import React from 'react'
import { Card } from './ui/Card'
import { Row } from './ui/Row'
import { Skeleton } from './ui/Skeleton'
import { EmptyState } from './ui/EmptyState'
import { api } from '../provider'

const { OpenInBrowser } = api
import type { AutopilotStatus, ActivePR } from '../types'

const STAGE_ICONS: Record<string, string> = {
  created: '+',
  waiting_ci: '~',
  ci_passed: '✓',
  ci_failed: '✗',
  awaiting_approval: '?',
  merging: '›',
  releasing: '↑',
  failed: '!',
}

// In-progress stages read accent (steel); amber is reserved for the one
// needs-attention stage (awaiting_approval); rose for terminal failures.
const STAGE_COLORS: Record<string, string> = {
  created: 'text-muted',
  waiting_ci: 'text-accent',
  ci_passed: 'text-success',
  ci_failed: 'text-danger',
  awaiting_approval: 'text-warning',
  merging: 'text-accent',
  releasing: 'text-accent',
  failed: 'text-danger',
}

interface PRRowProps {
  pr: ActivePR
}

function PRRow({ pr }: PRRowProps) {
  const icon = STAGE_ICONS[pr.stage] ?? '?'
  const color = STAGE_COLORS[pr.stage] ?? 'text-muted'
  const stageLabel = pr.stage.replace('_', ' ')

  return (
    <Row
      onActivate={pr.url ? () => OpenInBrowser(pr.url) : undefined}
      disabled={!pr.url}
      ariaLabel={`Open PR #${pr.number} for ${pr.branchName} — ${stageLabel}`}
    >
      <span aria-hidden="true" className={`text-sm font-semibold w-4 text-center shrink-0 ${color}`}>
        {icon}
      </span>
      <span className="text-accent text-meta tabular-nums shrink-0">#{pr.number}</span>
      <span className="text-secondary text-sm truncate flex-1 min-w-0">{pr.branchName}</span>
      <span className={`text-meta shrink-0 ${color}`}>{stageLabel}</span>
    </Row>
  )
}

function DotLeaderRow({
  label,
  value,
  valueColor = 'text-secondary',
}: {
  label: string
  value: string
  valueColor?: string
}) {
  return (
    <div className="flex items-baseline text-sm px-2.5">
      <span className="text-faint shrink-0">{label}</span>
      <span
        className="flex-1 overflow-hidden whitespace-nowrap text-fill mx-1.5"
        style={{ lineHeight: '1' }}
        aria-hidden="true"
      >
        {'·'.repeat(80)}
      </span>
      <span className={`shrink-0 tabular-nums ${valueColor}`}>{value}</span>
    </div>
  )
}

interface AutopilotPanelProps {
  status: AutopilotStatus
  loaded: boolean
}

export function AutopilotPanel({ status, loaded }: AutopilotPanelProps) {
  const inactive = !status.enabled && status.activePRs.length === 0

  return (
    <Card title="Autopilot" className="shrink-0">
      <div className="overflow-y-auto h-full log-scroll">
        {!loaded ? (
          <Skeleton rows={4} />
        ) : inactive ? (
          <EmptyState message="Autopilot inactive" />
        ) : (
          <div className="flex flex-col gap-1">
            <DotLeaderRow label="Environment" value={status.environment || 'dev'} />
            <DotLeaderRow label="Post-merge" value={status.autoRelease ? 'auto-release' : 'none'} />
            <DotLeaderRow
              label="Auto-release"
              value={status.autoRelease ? 'enabled' : 'disabled'}
              valueColor={status.autoRelease ? 'text-success' : 'text-faint'}
            />
            <DotLeaderRow label="Active PRs" value={String(status.activePRs.length)} />
            {status.failureCount > 0 && (
              <DotLeaderRow label="Failures" value={String(status.failureCount)} valueColor="text-warning" />
            )}
            {status.activePRs.length > 0 && (
              <div className="mt-1.5 flex flex-col gap-0.5 border-t border-border pt-1.5 -mx-1.5">
                {status.activePRs.map((pr) => (
                  <PRRow key={pr.number} pr={pr} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </Card>
  )
}
