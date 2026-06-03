import React from 'react'
import { Button } from './ui/Button'

interface HeaderProps {
  serverRunning: boolean
  version?: string
  starting?: boolean
  error?: string
  onStartGateway?: () => void
}

function Wordmark() {
  // Compact SVG wordmark; decorative — the accessible name comes from the
  // adjacent sr-only <h1>.
  return (
    <svg
      width="62"
      height="20"
      viewBox="0 0 124 40"
      role="img"
      aria-hidden="true"
      className="text-accent"
    >
      <g fill="currentColor">
        <path d="M0 4h14a12 12 0 0 1 0 24H7v8H0V4zm7 6v12h7a6 6 0 0 0 0-12H7z" />
        <rect x="34" y="4" width="7" height="32" rx="2" />
        <path d="M52 4h7v26h15v6H52V4z" />
        <rect x="82" y="14" width="7" height="22" rx="2" />
        <circle cx="85.5" cy="6.5" r="4" />
        <path d="M100 4h7v10h8v6h-8v9a2 2 0 0 0 2 2h6v5h-8a7 7 0 0 1-7-7V4z" />
      </g>
    </svg>
  )
}

export function Header({ serverRunning, version, starting = false, error, onStartGateway }: HeaderProps) {
  const daemonLabel = starting ? 'Daemon starting' : serverRunning ? 'Daemon running' : 'Daemon offline'
  const dotClass = starting
    ? 'bg-accent pulse'
    : serverRunning
    ? 'bg-success pulse'
    : 'bg-faint'
  const textClass = starting ? 'text-accent' : serverRunning ? 'text-success' : 'text-faint'

  return (
    <header className="titlebar flex items-center gap-4 px-4 py-3 border-b border-border bg-card shrink-0">
      <h1 className="sr-only">Pilot</h1>
      <Wordmark />

      <div className="flex items-center gap-4 flex-1 min-w-0">
        {version && <span className="text-faint text-[12px] font-mono">{version}</span>}

        <span role="status" className={`flex items-center gap-1.5 text-[12px] ${textClass}`}>
          <span className={`inline-block w-2 h-2 rounded-full ${dotClass}`} />
          <span aria-hidden="true">
            {starting ? 'daemon starting' : serverRunning ? 'daemon running' : 'daemon offline'}
          </span>
          <span className="sr-only">{daemonLabel}</span>
        </span>

        {error && (
          <span className="selectable text-danger text-[12px] truncate" role="alert">
            {error}
          </span>
        )}
      </div>

      {!serverRunning && onStartGateway && (
        <Button variant="secondary" size="sm" disabled={starting} onClick={onStartGateway}>
          Start
        </Button>
      )}
    </header>
  )
}
