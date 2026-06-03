import React, { useRef, useEffect, useState } from 'react'
import { Card } from './ui/Card'
import { EmptyState } from './ui/EmptyState'
import { COLORS } from './ui/colors'
import type { GitGraphData, GitGraphLine } from '../types'

// Track colors mirror the TUI gitgraph.go palette. Track 0 and the HEAD/branch
// ref badges reuse the shared design tokens so the accent stays in lockstep
// with the rest of the desktop chrome (no inline hex re-declarations).
const TRACK_COLORS = [COLORS.accent, COLORS.success, COLORS.warning, COLORS.danger, COLORS.muted]

const HEAD_COLOR = COLORS.accent
const TAG_COLOR = COLORS.warning
const BRANCH_COLOR = COLORS.success

// Graph characters that indicate track boundaries
const GRAPH_CHARS = new Set(['*', '●', '|', '│', '\\', '/', '╮', '╯', '╰', '╭', '─', '╌'])

interface GitGraphPanelProps {
  data: GitGraphData
}

function trackColorAt(col: number): string {
  const track = Math.floor(col / 2)
  return TRACK_COLORS[track % TRACK_COLORS.length]
}

/** Colorize graph characters by track position */
function renderGraphChars(chars: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = []
  for (let i = 0; i < chars.length; i++) {
    const ch = chars[i]
    if (GRAPH_CHARS.has(ch)) {
      nodes.push(
        <span key={i} style={{ color: trackColorAt(i) }}>
          {ch}
        </span>
      )
    } else {
      nodes.push(<span key={i}>{ch}</span>)
    }
  }
  return nodes
}

/** Parse and colorize ref decorations */
function renderRefs(refs: string): React.ReactNode {
  if (!refs) return null

  // Strip outer parens if present: "(HEAD -> main, tag: v1.0)" → "HEAD -> main, tag: v1.0"
  let inner = refs.trim()
  if (inner.startsWith('(') && inner.endsWith(')')) {
    inner = inner.slice(1, -1)
  }

  const parts = inner.split(',').map((s) => s.trim()).filter(Boolean)
  const nodes: React.ReactNode[] = []

  for (let i = 0; i < parts.length; i++) {
    let part = parts[i]
    // Clean prefixes
    part = part.replace(/^refs\/remotes\//, '').replace(/^refs\/heads\//, '').replace(/^refs\//, '')

    if (i > 0) nodes.push(<span key={`sep-${i}`} className="text-muted">{', '}</span>)

    if (part.startsWith('HEAD')) {
      nodes.push(
        <span key={i} style={{ color: HEAD_COLOR, fontWeight: 600 }}>
          {part}
        </span>
      )
    } else if (part.startsWith('tag:')) {
      nodes.push(
        <span key={i} style={{ color: TAG_COLOR, fontWeight: 600 }}>
          {part}
        </span>
      )
    } else {
      nodes.push(
        <span key={i} style={{ color: BRANCH_COLOR }}>
          {part}
        </span>
      )
    }
  }

  return (
    <span>
      <span className="text-muted">{'('}</span>
      {nodes}
      <span className="text-muted">{')'}</span>
      {' '}
    </span>
  )
}

function GraphLine({ line }: { line: GitGraphLine }) {
  return (
    <div className="flex gap-0 leading-snug whitespace-pre py-0.5">
      <span className="shrink-0">{renderGraphChars(line.graph_chars)}</span>
      {line.refs && <span className="shrink-0 ml-1">{renderRefs(line.refs)}</span>}
      {line.message && <span className="text-secondary ml-1 truncate">{line.message}</span>}
      {line.sha && <span className="text-muted ml-2 shrink-0 tabular-nums">{line.sha}</span>}
      {line.author && <span className="text-faint ml-2 shrink-0">{line.author}</span>}
    </div>
  )
}

/** Plain-text commit summary for assistive tech, since the colored glyph art
 * itself is decorative (aria-hidden). */
function commitSummary(lines: GitGraphLine[]): string {
  return lines
    .filter((l) => l.message || l.sha)
    .map((l) => [l.sha, l.message, l.author].filter(Boolean).join(' '))
    .join('; ')
}

export function GitGraphPanel({ data }: GitGraphPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [visibleEnd, setVisibleEnd] = useState(0)

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return

    function updateCounter() {
      if (!el) return
      const lineH = 21 // approx line height at text-sm
      const end = Math.min(
        Math.ceil((el.scrollTop + el.clientHeight) / lineH),
        data.lines.length
      )
      setVisibleEnd(end)
    }

    updateCounter()
    el.addEventListener('scroll', updateCounter)
    return () => el.removeEventListener('scroll', updateCounter)
  }, [data.lines.length])

  return (
    <Card title="Git Graph" className="flex-1 min-h-0">
      <div className="flex flex-col h-full min-h-0">
        <div
          ref={scrollRef}
          className="flex-1 overflow-y-auto log-scroll text-sm min-h-0 selectable"
        >
          {data.lines.length === 0 ? (
            <EmptyState message={data.error ? data.error : 'No git graph data'} />
          ) : (
            <>
              <span className="sr-only">{commitSummary(data.lines)}</span>
              <div aria-hidden="true">
                {data.lines.map((line, i) => (
                  <GraphLine key={i} line={line} />
                ))}
              </div>
            </>
          )}
        </div>
        {data.total_count > 0 && (
          <div className="shrink-0 text-faint text-meta tabular-nums border-t border-border pt-1.5 mt-1.5">
            {visibleEnd} of {data.total_count}
          </div>
        )}
      </div>
    </Card>
  )
}
