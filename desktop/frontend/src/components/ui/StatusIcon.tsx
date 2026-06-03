import React from 'react'
import { Status, STATUS_ICON, STATUS_TEXT_COLOR } from './status'

interface StatusIconProps {
  status: Status
  className?: string
}

export function StatusIcon({ status, className = '' }: StatusIconProps) {
  const icon = STATUS_ICON[status] ?? '?'
  const color = STATUS_TEXT_COLOR[status] ?? 'text-muted'
  const pulse = status === 'running' ? 'pulse' : ''

  return (
    <span
      aria-hidden="true"
      className={`inline-block w-4 text-center ${color} ${pulse} ${className}`.trim()}
    >
      {icon}
    </span>
  )
}
