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
  // Text wordmark; decorative — the accessible name comes from the adjacent
  // sr-only <h1>. Rendered in the brand accent via the app's Geist/system font.
  return (
    <span
      aria-hidden="true"
      className="text-accent text-[18px] font-semibold tracking-tight leading-none select-none"
    >
      Pilot
    </span>
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
