import React from 'react'

const PILOT_LOGO = `██████╗ ██╗██╗      ██████╗ ████████╗
██╔══██╗██║██║     ██╔═══██╗╚══██╔══╝
██████╔╝██║██║     ██║   ██║   ██║
██╔═══╝ ██║██║     ██║   ██║   ██║
██║     ██║███████╗╚██████╔╝   ██║
╚═╝     ╚═╝╚══════╝ ╚═════╝    ╚═╝`

interface HeaderProps {
  serverRunning: boolean
  version?: string
  starting?: boolean
  error?: string
  onStartGateway?: () => void
}

export function Header({ serverRunning, version, starting = false, error, onStartGateway }: HeaderProps) {
  return (
    <div className="px-3 py-2 border-b border-border">
      <pre
        className="leading-none text-[8px] font-mono whitespace-pre"
        style={{ color: '#7eb8da' }}
      >
        {PILOT_LOGO}
      </pre>
      <div className="flex items-center gap-3 mt-1">
        {version && (
          <span className="text-gray text-[10px] font-mono">
            {version}
          </span>
        )}
        <span className={`flex items-center gap-1 text-[10px] ${serverRunning ? 'text-sage' : 'text-gray'}`}>
          <span className={`inline-block w-1.5 h-1.5 rounded-full ${serverRunning ? 'bg-sage pulse' : 'bg-gray'}`} />
          {starting ? 'daemon starting' : serverRunning ? 'daemon running' : 'daemon offline'}
        </span>
        {!serverRunning && onStartGateway && (
          <button
            className="px-1.5 py-0.5 border border-border bg-card text-gray text-[10px] hover:border-steel hover:text-lightgray disabled:text-slate disabled:hover:border-border"
            disabled={starting}
            type="button"
            onClick={onStartGateway}
          >
            Start
          </button>
        )}
        {error && <span className="text-rose text-[10px] truncate">{error}</span>}
      </div>
    </div>
  )
}
