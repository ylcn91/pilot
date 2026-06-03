import React from 'react'
import { Status, STATUS_FILL } from './status'

interface ProgressBarProps {
  status: Status
  progress: number // 0.0 - 1.0
  shimmerDelay?: number // 0-4
  className?: string
}

export function ProgressBar({ status, progress, shimmerDelay = 0, className = '' }: ProgressBarProps) {
  const w = className || 'w-20'
  const pct = Math.max(0, Math.min(1, progress)) * 100
  const valueNow = Math.round(pct)

  const ariaProps = {
    role: 'progressbar' as const,
    'aria-valuenow': valueNow,
    'aria-valuemin': 0,
    'aria-valuemax': 100,
    'aria-label': 'Task progress',
  }

  if (status === 'queued') {
    const delayClass = `shimmer-delay-${Math.min(shimmerDelay, 4)}`
    return (
      <div {...ariaProps} className={`inline-block ${w} h-2 rounded-md shimmer-bar ${delayClass}`} />
    )
  }

  if (status === 'pending') {
    return (
      <div {...ariaProps} className={`inline-flex ${w} h-2 rounded-md bg-fill overflow-hidden`} />
    )
  }

  const fillColor = STATUS_FILL[status] ?? 'bg-accent'

  return (
    <div {...ariaProps} className={`inline-flex ${w} h-2 rounded-md bg-fill overflow-hidden`}>
      <div className={`h-full ${fillColor}`} style={{ width: `${pct}%` }} />
    </div>
  )
}
