// Single source of truth for task status presentation. StatusIcon,
// QueuePanel and ProgressBar all import from here so colors/icons stay in
// lockstep. In-progress states are consistently accent (steel); amber is
// reserved for warnings only, never for "in progress".

export type Status = 'done' | 'running' | 'queued' | 'pending' | 'failed'

export const STATUS_TEXT_COLOR: Record<Status, string> = {
  done: 'text-success',
  running: 'text-accent',
  queued: 'text-muted',
  pending: 'text-faint',
  failed: 'text-danger',
}

export const STATUS_ICON: Record<Status, string> = {
  done: '✓',
  running: '●',
  queued: '◌',
  pending: '·',
  failed: '✗',
}

export const STATUS_FILL: Record<Status, string> = {
  done: 'bg-success',
  running: 'bg-accent',
  queued: 'bg-fill',
  pending: 'bg-fill',
  failed: 'bg-danger',
}
