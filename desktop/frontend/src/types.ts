export interface DashboardMetrics {
  totalTokens: number
  inputTokens: number
  outputTokens: number
  totalCostUSD: number
  totalTasks: number
  succeededTasks: number
  failedTasks: number
  tokenSparkline: number[]
  costSparkline: number[]
  queueSparkline: number[]
}

export interface QueueTask {
  id: string
  issueID: string
  title: string
  status: 'running' | 'queued' | 'pending' | 'done' | 'failed'
  progress: number
  prURL?: string
  issueURL?: string
  projectPath: string
  createdAt: string
}

export interface HistoryEntry {
  id: string
  issueID: string
  title: string
  status: string
  prURL?: string
  projectPath: string
  completedAt: string
  durationMs: number
  epicID?: string
  subIssues?: HistoryEntry[]
}

export interface ActivePR {
  number: number
  url: string
  stage: string
  ciStatus?: string
  error?: string
  branchName: string
}

export interface AutopilotStatus {
  enabled: boolean
  environment: string
  autoRelease: boolean
  activePRs: ActivePR[]
  failureCount: number
}

export interface LogEntry {
  ts: string
  level?: 'info' | 'warn' | 'error'
  message: string
  component?: string
}

export interface ServerStatus {
  running: boolean
  version?: string
  gatewayURL?: string
}

export interface GitGraphLine {
  graph_chars: string
  refs?: string
  message?: string
  author?: string
  sha?: string
}

export interface GitGraphData {
  lines: GitGraphLine[]
  total_count: number
  error?: string
  last_refresh: string
}

export type GatewayMessageType = 'task' | 'status' | 'progress' | 'ping' | 'pong'

export interface GatewayMessage<TPayload = unknown> {
  type: GatewayMessageType
  payload: TPayload
}

export type RuntimeSandbox = 'read-only' | 'workspace-write' | 'danger-full-access'

export interface RuntimeStartPayload {
  action: 'codexruntime.start'
  prompt: string
  cwd?: string
  model?: string
  sandbox?: RuntimeSandbox
}

export interface RuntimeTurnPayload {
  action: 'codexruntime.turn'
  prompt: string
}

export interface RuntimeStopPayload {
  action: 'codexruntime.stop'
}

export interface RuntimeApprovalResponsePayload {
  action: 'codexruntime.approval.respond'
  requestId: number | string
  decision?: string
  scope?: string
}

export interface CodexRuntimeEvent {
  type: string
  method: string
  threadId?: string
  turnId?: string
  itemId?: string
  delta?: string
  diff?: string
  error?: string
  explanation?: string
  status?: unknown
  plan?: unknown
  changes?: unknown
  rawParams?: unknown
}

export interface RuntimeProgressPayload {
  source: 'codexruntime'
  kind: 'event' | 'approval_request' | 'error'
  event?: CodexRuntimeEvent
  error?: string
  requestId?: number | string
  method?: string
  params?: unknown
  choices?: string[]
}

export interface RuntimeApprovalRequest {
  requestId: number | string
  method: string
  params?: unknown
  choices: string[]
}

export interface CodexChatMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  text: string
}
