// Wails v2 runtime bindings.
// The Go App methods are accessible via window.go.main.App.*
// These wrappers provide TypeScript type safety.

import type { DashboardMetrics, QueueTask, HistoryEntry, AutopilotStatus, ServerStatus, LogEntry, GitGraphData, Finding } from './types'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare const window: any

function goCall<T>(method: string, ...args: unknown[]): Promise<T> {
  if (typeof window !== 'undefined' && window.go?.main?.App?.[method]) {
    return window.go.main.App[method](...args) as Promise<T>
  }
  // Development fallback — return empty value
  return Promise.resolve(undefined as unknown as T)
}

// Go marshals nil slices to JSON `null`, not `[]`. Coerce every slice field
// back to an array at the binding boundary so components can call .length/.map
// without null guards (browser/HTTP mode already returns `[]`).
export function GetMetrics(): Promise<DashboardMetrics> {
  return goCall<DashboardMetrics>('GetMetrics').then((m) =>
    m
      ? {
          ...m,
          tokenSparkline: m.tokenSparkline ?? [],
          costSparkline: m.costSparkline ?? [],
          queueSparkline: m.queueSparkline ?? [],
        }
      : m,
  )
}

export function GetQueueTasks(): Promise<QueueTask[]> {
  return goCall<QueueTask[]>('GetQueueTasks').then((q) => q ?? [])
}

export function GetHistory(limit: number): Promise<HistoryEntry[]> {
  return goCall<HistoryEntry[]>('GetHistory', limit).then((h) => h ?? [])
}

export function GetAutopilotStatus(): Promise<AutopilotStatus> {
  return goCall<AutopilotStatus>('GetAutopilotStatus').then((s) =>
    s ? { ...s, activePRs: s.activePRs ?? [] } : s,
  )
}

export function GetServerStatus(): Promise<ServerStatus> {
  return goCall<ServerStatus>('GetServerStatus')
}

export function EnsureGatewayRunning(): Promise<ServerStatus> {
  return goCall<ServerStatus>('EnsureGatewayRunning')
}

export function GetLogs(limit: number): Promise<LogEntry[]> {
  return goCall<LogEntry[]>('GetLogs', limit).then((l) => l ?? [])
}

export function GetGitGraph(limit: number): Promise<GitGraphData> {
  return goCall<GitGraphData>('GetGitGraph', limit).then((g) =>
    g ? { ...g, lines: g.lines ?? [] } : g,
  )
}

export function GetArchitectFindings(): Promise<Finding[]> {
  return goCall<Finding[]>('GetArchitectFindings').then((f) => f ?? [])
}

export function OpenInBrowser(url: string): Promise<void> {
  return goCall<void>('OpenInBrowser', url)
}
