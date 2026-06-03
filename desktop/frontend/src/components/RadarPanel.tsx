import React, { useId, useState } from 'react'
import { Card } from './ui/Card'
import { Skeleton } from './ui/Skeleton'
import { EmptyState } from './ui/EmptyState'
import type { Finding, RiskLevel } from '../types'

// Risk badge colors: cool/success for low severity, amber for the
// needs-attention middle, rose for the dangerous end.
const RISK_COLORS: Record<string, string> = {
  low: 'text-success',
  medium: 'text-warning',
  high: 'text-danger',
  'release-blocker': 'text-danger',
}

const RISK_LABELS: Record<string, string> = {
  low: 'LOW',
  medium: 'MED',
  high: 'HIGH',
  'release-blocker': 'BLOCK',
}

// Rank used to surface the most dangerous findings first.
const RISK_ORDER: Record<string, number> = {
  'release-blocker': 0,
  high: 1,
  medium: 2,
  low: 3,
}

function riskColor(risk: RiskLevel | string): string {
  return RISK_COLORS[risk] ?? 'text-muted'
}

function riskLabel(risk: RiskLevel | string): string {
  return RISK_LABELS[risk] ?? String(risk || '?').toUpperCase()
}

interface FindingRowProps {
  finding: Finding
  expanded: boolean
  onToggle: () => void
}

function FindingRow({ finding, expanded, onToggle }: FindingRowProps) {
  const color = riskColor(finding.risk)
  const prPieces = finding.suggested_pr_pieces ?? []
  const files = finding.files ?? []
  const bodyId = useId()

  return (
    <div className="border-b border-border/50 last:border-b-0">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={bodyId}
        onClick={onToggle}
        className="group flex items-center gap-2 w-full text-left px-2.5 py-1.5 h-8 rounded-md bg-transparent hover:bg-raised active:bg-raised/80 transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card cursor-pointer"
      >
        <span className={`text-meta font-semibold w-10 shrink-0 ${color}`}>{riskLabel(finding.risk)}</span>
        {finding.kind && <span className="text-accent text-meta shrink-0">[{finding.kind}]</span>}
        <span className="text-secondary text-sm truncate flex-1 min-w-0">{finding.title}</span>
        <span aria-hidden="true" className="text-muted text-sm shrink-0">
          {expanded ? '−' : '+'}
        </span>
      </button>

      {expanded && (
        <div id={bodyId} className="px-2.5 pb-2 pt-1 flex flex-col gap-1.5">
          {finding.why_it_matters && (
            <p className="text-muted text-sm leading-snug">{finding.why_it_matters}</p>
          )}
          {prPieces.length > 0 && (
            <div className="text-sm">
              <span className="text-faint">PR pieces:</span>
              <ul className="list-disc list-inside text-muted">
                {prPieces.map((piece, i) => (
                  <li key={i} className="truncate">
                    {piece}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {finding.test_plan && (
            <div className="text-sm">
              <span className="text-faint">Test plan: </span>
              <span className="text-muted">{finding.test_plan}</span>
            </div>
          )}
          {files.length > 0 && (
            <div className="text-sm">
              <span className="text-faint">Files:</span>
              <ul className="text-accent selectable">
                {files.map((file, i) => (
                  <li key={i} className="truncate">
                    {file}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

interface RadarPanelProps {
  findings: Finding[]
  loaded: boolean
}

export function RadarPanel({ findings, loaded }: RadarPanelProps) {
  const [expanded, setExpanded] = useState<Set<number>>(new Set())

  const sorted = [...findings].sort((a, b) => {
    const oa = RISK_ORDER[a.risk] ?? 99
    const ob = RISK_ORDER[b.risk] ?? 99
    return oa - ob
  })

  function toggle(index: number) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(index)) {
        next.delete(index)
      } else {
        next.add(index)
      }
      return next
    })
  }

  return (
    <Card title={`Architect · ${findings.length}`} className="shrink-0">
      <div className="overflow-y-auto h-full log-scroll -mx-1.5">
        {!loaded ? (
          <Skeleton rows={3} />
        ) : sorted.length === 0 ? (
          <EmptyState message="No findings" />
        ) : (
          <div className="flex flex-col">
            {sorted.map((finding, index) => (
              <FindingRow
                key={index}
                finding={finding}
                expanded={expanded.has(index)}
                onToggle={() => toggle(index)}
              />
            ))}
          </div>
        )}
      </div>
    </Card>
  )
}
