import React from 'react'

interface EmptyStateProps {
  message: string
  icon?: React.ReactNode
  className?: string
}

// Canonical empty-state, rendered only when data has loaded and is empty
// (never during the loading phase — use Skeleton for that).
export function EmptyState({ message, icon, className = '' }: EmptyStateProps) {
  return (
    <div
      className={`flex flex-col items-center justify-center gap-2 py-8 px-4 text-center text-muted text-[13px] ${className}`.trim()}
    >
      {icon && (
        <span aria-hidden="true" className="text-faint">
          {icon}
        </span>
      )}
      <span>{message}</span>
    </div>
  )
}
