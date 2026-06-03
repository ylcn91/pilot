import React, { useState } from 'react'
import { Card } from './ui/Card'
import type { Finding, RiskLevel } from '../types'

// Risk badge colors mirror the autopilot stage palette: cool tones for low
// severity, warm/red for the dangerous end. Unknown levels fall back to gray.
const RISK_COLORS: Record<string, string> = {
  low: 'text-sage',
  medium: 'text-amber',
  high: 'text-rose',
  'release-blocker': 'text-rose',
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
  return RISK_COLORS[risk] ?? 'text-midgray'
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

  return (
    <div className="border-b border-border/50 last:border-b-0">
      <div
        className="flex items-center gap-1.5 cursor-pointer hover:bg-slate/30 rounded px-1 py-px transition-colors"
        onClick={onToggle}
      >
        <span className={`text-[9px] font-bold w-9 shrink-0 ${color}`}>
          {riskLabel(finding.risk)}
        </span>
        {finding.kind && (
          <span className="text-steel text-[10px] shrink-0">[{finding.kind}]</span>
        )}
        <span className="text-lightgray text-[10px] truncate flex-1 min-w-0">
          {finding.title}
        </span>
        <span className="text-midgray text-[10px] shrink-0">{expanded ? '−' : '+'}</span>
      </div>

      {expanded && (
        <div className="px-2 pb-1 pt-0.5 space-y-0.5">
          {finding.why_it_matters && (
            <p className="text-midgray text-[10px] leading-snug">{finding.why_it_matters}</p>
          )}
          {prPieces.length > 0 && (
            <div className="text-[10px]">
              <span className="text-gray">PR pieces:</span>
              <ul className="list-disc list-inside text-midgray">
                {prPieces.map((piece, i) => (
                  <li key={i} className="truncate">{piece}</li>
                ))}
              </ul>
            </div>
          )}
          {finding.test_plan && (
            <div className="text-[10px]">
              <span className="text-gray">Test plan: </span>
              <span className="text-midgray">{finding.test_plan}</span>
            </div>
          )}
          {files.length > 0 && (
            <div className="text-[10px]">
              <span className="text-gray">Files:</span>
              <ul className="text-steel">
                {files.map((file, i) => (
                  <li key={i} className="truncate">{file}</li>
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
}

export function RadarPanel({ findings }: RadarPanelProps) {
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
    <Card title={`ARCHITECT  ${findings.length}`} className="shrink-0">
      <div className="overflow-y-auto h-full log-scroll">
        {sorted.length === 0 ? (
          <div className="text-gray text-[10px] px-1 py-1">no findings</div>
        ) : (
          <div className="space-y-0">
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
