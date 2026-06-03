import React from 'react'

interface RowProps {
  as?: 'button' | 'a'
  href?: string
  onActivate?: () => void
  selected?: boolean
  disabled?: boolean
  ariaLabel?: string
  className?: string
  children: React.ReactNode
}

const BASE =
  'group flex items-center gap-3 w-full text-left px-2.5 py-1.5 h-8 rounded-md bg-transparent hover:bg-raised active:bg-raised/80 transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer'

// Row is the canonical interactive list row. It renders a real <a href> when
// a destination is given, otherwise a <button type="button"> — both keyboard
// accessible natively (Enter/Space), no manual onKeyDown handling required.
export function Row({
  as,
  href,
  onActivate,
  selected = false,
  disabled = false,
  ariaLabel,
  className = '',
  children,
}: RowProps) {
  const selectedClass = selected ? 'bg-raised' : ''
  const cls = `${BASE} ${selectedClass} ${className}`.trim()
  const renderAnchor = (as ?? (href ? 'a' : 'button')) === 'a' && !!href

  if (renderAnchor) {
    return (
      <a
        href={href}
        aria-label={ariaLabel}
        aria-current={selected ? 'true' : undefined}
        className={cls}
        onClick={onActivate}
      >
        {children}
      </a>
    )
  }

  return (
    <button
      type="button"
      aria-label={ariaLabel}
      aria-pressed={selected ? true : undefined}
      disabled={disabled}
      className={cls}
      onClick={onActivate}
    >
      {children}
    </button>
  )
}
