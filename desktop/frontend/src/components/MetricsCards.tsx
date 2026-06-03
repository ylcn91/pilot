import React from 'react'
import { Card } from './ui/Card'
import { Sparkline } from './ui/Sparkline'
import { COLORS } from './ui/colors'
import type { DashboardMetrics } from '../types'

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

function formatCost(usd: number): string {
  if (usd >= 1000) return `$${usd.toFixed(0)}`
  if (usd >= 1) return `$${usd.toFixed(2)}`
  return `$${usd.toFixed(3)}`
}

interface MetricCardProps {
  title: string
  value: string
  detail1: string
  detail2: string
  sparklineData: number[]
  sparklineColor: string
}

function MetricCard({ title, value, detail1, detail2, sparklineData, sparklineColor }: MetricCardProps) {
  return (
    <Card
      title={title}
      action={<span className="text-fg text-metric font-semibold tabular-nums">{value}</span>}
      className="min-w-0"
    >
      <div className="flex flex-col justify-between gap-2 h-full">
        <div className="flex flex-col gap-0.5">
          <div className="text-muted text-sm tabular-nums">{detail1}</div>
          <div className="text-muted text-sm tabular-nums">{detail2}</div>
        </div>
        <Sparkline data={sparklineData} color={sparklineColor} width={120} height={24} />
      </div>
    </Card>
  )
}

interface MetricsCardsProps {
  metrics: DashboardMetrics
}

export function MetricsCards({ metrics }: MetricsCardsProps) {
  const costPerTask =
    metrics.totalTasks > 0 ? formatCost(metrics.totalCostUSD / metrics.totalTasks) : '$0.000'

  return (
    <div className="grid grid-cols-3 gap-3">
      <MetricCard
        title="Tokens"
        value={formatTokens(metrics.totalTokens)}
        detail1={`↑ ${formatTokens(metrics.inputTokens)} input`}
        detail2={`↓ ${formatTokens(metrics.outputTokens)} output`}
        sparklineData={metrics.tokenSparkline}
        sparklineColor={COLORS.accent}
      />
      <MetricCard
        title="Cost"
        value={formatCost(metrics.totalCostUSD)}
        detail1={`${costPerTask}/task`}
        detail2={`${metrics.totalTasks} total tasks`}
        sparklineData={metrics.costSparkline}
        sparklineColor={COLORS.success}
      />
      <MetricCard
        title="Queue"
        value={String(metrics.totalTasks)}
        detail1={`✓ ${metrics.succeededTasks} done`}
        detail2={`✗ ${metrics.failedTasks} failed`}
        sparklineData={metrics.queueSparkline}
        sparklineColor={COLORS.muted}
      />
    </div>
  )
}
