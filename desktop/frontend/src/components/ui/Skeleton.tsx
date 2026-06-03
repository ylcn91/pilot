import React from 'react'

interface SkeletonProps {
  rows?: number
  className?: string
}

// Loading placeholder shown while data has not yet arrived (first poll).
// Renders `rows` shimmer bars using the shared shimmer-bar utility.
export function Skeleton({ rows = 4, className = '' }: SkeletonProps) {
  return (
    <div
      className={`flex flex-col gap-2 p-1 ${className}`.trim()}
      aria-hidden="true"
    >
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="h-4 rounded-md bg-fill shimmer-bar" />
      ))}
    </div>
  )
}
