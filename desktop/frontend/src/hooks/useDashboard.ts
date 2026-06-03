import { useState, useEffect, useRef } from 'react'
import type { DashboardMetrics, QueueTask, HistoryEntry, AutopilotStatus, ServerStatus, LogEntry, Finding } from '../types'
import { api } from '../provider'
import { useDashboardLogs } from './useWebSocket'

const { GetMetrics, GetQueueTasks, GetHistory, GetAutopilotStatus, GetServerStatus, GetArchitectFindings } = api

export interface DashboardState {
  metrics: DashboardMetrics
  queueTasks: QueueTask[]
  history: HistoryEntry[]
  autopilot: AutopilotStatus
  findings: Finding[]
  server: ServerStatus
  serverStarting: boolean
  logs: LogEntry[]
  loaded: boolean
  ensureGatewayRunning: () => Promise<void>
}

const defaultMetrics: DashboardMetrics = {
  totalTokens: 0,
  inputTokens: 0,
  outputTokens: 0,
  totalCostUSD: 0,
  totalTasks: 0,
  succeededTasks: 0,
  failedTasks: 0,
  tokenSparkline: [0, 0, 0, 0, 0, 0, 0],
  costSparkline: [0, 0, 0, 0, 0, 0, 0],
  queueSparkline: [0, 0, 0, 0, 0, 0, 0],
}

const defaultAutopilot: AutopilotStatus = {
  enabled: false,
  environment: '',
  autoRelease: false,
  activePRs: [],
  failureCount: 0,
}

const defaultServer: ServerStatus = {
  running: false,
}

// useDashboard polls Wails backend bindings at 1s (data) and 5s (server status) intervals.
export function useDashboard(): DashboardState {
  const [metrics, setMetrics] = useState<DashboardMetrics>(defaultMetrics)
  const [queueTasks, setQueueTasks] = useState<QueueTask[]>([])
  const [history, setHistory] = useState<HistoryEntry[]>([])
  const [autopilot, setAutopilot] = useState<AutopilotStatus>(defaultAutopilot)
  const [findings, setFindings] = useState<Finding[]>([])
  const [server, setServer] = useState<ServerStatus>(defaultServer)
  const [serverStarting, setServerStarting] = useState(false)
  const [loaded, setLoaded] = useState(false)

  // Logs are streamed via WebSocket (falls back to polling in Wails mode).
  const logs = useDashboardLogs()

  const tickRef = useRef(0)

  useEffect(() => {
    async function poll() {
      tickRef.current += 1
      const t = tickRef.current

      // Data: every 1 second
      try {
        const [m, q, h, ap, fnd] = await Promise.all([
          GetMetrics(),
          GetQueueTasks(),
          GetHistory(5),
          GetAutopilotStatus(),
          GetArchitectFindings(),
        ])
        if (m) setMetrics(m)
        if (q) setQueueTasks(q)
        if (h) setHistory(h)
        if (ap) setAutopilot(ap)
        if (fnd) setFindings(fnd)
      } catch {
        // Graceful degradation — keep previous values
      } finally {
        // First poll resolved (success or degraded): leave the loading phase
        // so panels swap skeletons for either data or canonical empty states.
        setLoaded(true)
      }

      // Server status: every 5 seconds
      if (t % 5 === 0) {
        try {
          const s = await GetServerStatus()
          if (s) setServer(s)
        } catch {
          // Ignore
        }
      }
    }

    // Initial load
    poll()

    const id = setInterval(poll, 1000)
    return () => clearInterval(id)
  }, [])

  async function ensureGatewayRunning() {
    setServerStarting(true)
    try {
      const s = await api.EnsureGatewayRunning()
      if (s) setServer(s)
    } catch {
      setServer((prev) => ({ ...prev, running: false, error: 'gateway start failed' }))
    } finally {
      setServerStarting(false)
    }
  }

  return { metrics, queueTasks, history, autopilot, findings, server, serverStarting, logs, loaded, ensureGatewayRunning }
}
