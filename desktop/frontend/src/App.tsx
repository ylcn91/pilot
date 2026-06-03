import React from 'react'
import { Header } from './components/Header'
import { MetricsCards } from './components/MetricsCards'
import { QueuePanel } from './components/QueuePanel'
import { AutopilotPanel } from './components/AutopilotPanel'
import { RadarPanel } from './components/RadarPanel'
import { HistoryPanel } from './components/HistoryPanel'
import { LogsPanel } from './components/LogsPanel'
import { GitGraphPanel } from './components/GitGraphPanel'
import { CodexChatPanel } from './components/CodexChatPanel'
import { useDashboard } from './hooks/useDashboard'
import { useGitGraph } from './hooks/useGitGraph'

function App() {
  const { metrics, queueTasks, history, autopilot, findings, server, serverStarting, logs, loaded, ensureGatewayRunning } = useDashboard()
  const gitGraph = useGitGraph()
  const isWails = !!(window as any).go?.main?.App

  return (
    <div className={`flex flex-col h-full bg-bg overflow-hidden ${isWails ? 'wails-mode' : 'browser-mode'}`}>
      <Header
        serverRunning={server.running}
        version={server.version}
        starting={serverStarting}
        error={server.error}
        onStartGateway={isWails ? ensureGatewayRunning : undefined}
      />

      <main className="flex-1 min-h-0 overflow-hidden p-4">
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 h-full min-h-0">
          {/* Primary column: metrics + the operational panels */}
          <div className="lg:col-span-2 flex flex-col gap-4 min-h-0">
            <div className="shrink-0">
              <MetricsCards metrics={metrics} />
            </div>
            <div className="flex-1 grow-[2] min-h-0 flex flex-col">
              <QueuePanel tasks={queueTasks} loaded={loaded} />
            </div>
            <div className="flex-none max-h-[18%] min-h-[88px] flex flex-col">
              <AutopilotPanel status={autopilot} loaded={loaded} />
            </div>
            <div className="flex-none max-h-[18%] min-h-[88px] flex flex-col">
              <RadarPanel findings={findings} loaded={loaded} />
            </div>
            <div className="flex-none max-h-[18%] min-h-[88px] flex flex-col">
              <HistoryPanel entries={history} loaded={loaded} />
            </div>
            <div className="flex-1 grow-[2] min-h-0 flex flex-col">
              <LogsPanel entries={logs} loaded={loaded} />
            </div>
          </div>

          {/* Secondary column: Codex + git graph */}
          <div className="lg:col-span-1 flex flex-col gap-4 min-h-0">
            <div className="flex-1 grow-[2] min-h-0 flex flex-col">
              <CodexChatPanel gatewayURL={server.gatewayURL} projectPath={server.projectPath} />
            </div>
            <div className="flex-1 grow-[2] min-h-0 flex flex-col">
              <GitGraphPanel data={gitGraph} />
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}

export default App
