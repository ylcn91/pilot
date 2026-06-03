import React, { useId } from 'react'

interface CardProps {
  title: string
  action?: React.ReactNode
  children: React.ReactNode
  className?: string
}

export function Card({ title, action, children, className = '' }: CardProps) {
  const titleId = useId()
  return (
    <section
      aria-labelledby={titleId}
      className={`border border-border rounded-lg bg-card shadow-[0_1px_2px_rgba(0,0,0,0.3)] flex flex-col overflow-hidden ${className}`.trim()}
    >
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border shrink-0">
        <h2
          id={titleId}
          className="text-[13px] font-medium uppercase tracking-wide text-muted"
        >
          {title}
        </h2>
        {action}
      </div>
      <div className="flex-1 p-4 min-h-0 overflow-hidden">{children}</div>
    </section>
  )
}
