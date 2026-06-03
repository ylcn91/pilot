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

export function GetMetrics(): Promise<DashboardMetrics> {
  return goCall<DashboardMetrics>('GetMetrics')
}

export function GetQueueTasks(): Promise<QueueTask[]> {
  return goCall<QueueTask[]>('GetQueueTasks')
}

export function GetHistory(limit: number): Promise<HistoryEntry[]> {
  return goCall<HistoryEntry[]>('GetHistory', limit)
}

export function GetAutopilotStatus(): Promise<AutopilotStatus> {
  return goCall<AutopilotStatus>('GetAutopilotStatus')
}

export function GetServerStatus(): Promise<ServerStatus> {
  return goCall<ServerStatus>('GetServerStatus')
}

export function EnsureGatewayRunning(): Promise<ServerStatus> {
  return goCall<ServerStatus>('EnsureGatewayRunning')
}

export function GetLogs(limit: number): Promise<LogEntry[]> {
  return goCall<LogEntry[]>('GetLogs', limit)
}

export function GetGitGraph(limit: number): Promise<GitGraphData> {
  return goCall<GitGraphData>('GetGitGraph', limit)
}

export function GetArchitectFindings(): Promise<Finding[]> {
  return goCall<Finding[]>('GetArchitectFindings').then((f) => f ?? [])
}

export function OpenInBrowser(url: string): Promise<void> {
  return goCall<void>('OpenInBrowser', url)
}
