package memory

import (
	"strings"
	"time"
)

// ExecutionMetrics holds detailed metrics for a single execution
type ExecutionMetrics struct {
	ExecutionID      string
	TokensInput      int64
	TokensOutput     int64
	TokensTotal      int64
	EstimatedCostUSD float64
	FilesChanged     int
	LinesAdded       int
	LinesRemoved     int
	ModelName        string
	// GH-3028: RSS telemetry — zero on non-Linux/darwin or before Step 1 deploy.
	PeakRSSMB  int
	FinalRSSMB int
}

// MetricsQuery holds parameters for querying metrics
type MetricsQuery struct {
	Start    time.Time
	End      time.Time
	Projects []string // Empty = all projects
}

// MetricsSummary holds aggregated execution metrics
type MetricsSummary struct {
	// Counts
	TotalExecutions int
	SuccessCount    int
	FailedCount     int
	SuccessRate     float64

	// Duration
	TotalDurationMs int64
	AvgDurationMs   int64
	MinDurationMs   int64
	MaxDurationMs   int64

	// Tokens
	TotalTokensInput  int64
	TotalTokensOutput int64
	TotalTokens       int64
	AvgTokensPerTask  int64

	// Cost
	TotalCostUSD float64
	AvgCostUSD   float64

	// Code changes
	TotalFilesChanged int
	TotalLinesAdded   int
	TotalLinesRemoved int

	// PRs
	PRsCreated int

	// Time period
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// DailyMetrics holds metrics aggregated by day
type DailyMetrics struct {
	Date            time.Time
	ExecutionCount  int
	SuccessCount    int
	FailedCount     int
	TotalDurationMs int64
	TotalTokens     int64
	TotalCostUSD    float64
	FilesChanged    int
	LinesAdded      int
	LinesRemoved    int
	PRsCreated      int
}

// ProjectMetrics holds metrics aggregated by project
type ProjectMetrics struct {
	ProjectPath     string
	ProjectName     string
	ExecutionCount  int
	SuccessCount    int
	FailedCount     int
	SuccessRate     float64
	TotalDurationMs int64
	TotalTokens     int64
	TotalCostUSD    float64
	LastExecution   time.Time
}

// FailureReason holds failure breakdown data
type FailureReason struct {
	Reason string
	Count  int
}

// ExportedExecution represents an execution record for export
type ExportedExecution struct {
	ID               string     `json:"id"`
	TaskID           string     `json:"task_id"`
	ProjectPath      string     `json:"project_path"`
	Status           string     `json:"status"`
	DurationMs       int64      `json:"duration_ms"`
	TokensInput      int64      `json:"tokens_input"`
	TokensOutput     int64      `json:"tokens_output"`
	TokensTotal      int64      `json:"tokens_total"`
	EstimatedCostUSD float64    `json:"estimated_cost_usd"`
	FilesChanged     int        `json:"files_changed"`
	LinesAdded       int        `json:"lines_added"`
	LinesRemoved     int        `json:"lines_removed"`
	ModelName        string     `json:"model_name"`
	PRUrl            string     `json:"pr_url,omitempty"`
	CommitSHA        string     `json:"commit_sha,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// Model pricing constants (USD per 1M tokens)
// Source: https://platform.claude.com/docs/en/about-claude/pricing
const (
	// Claude Sonnet 4.5/4 pricing
	SonnetInputPricePerMillion  = 3.00
	SonnetOutputPricePerMillion = 15.00

	// Claude Opus 4.6/4.5 pricing (same price)
	OpusInputPricePerMillion  = 5.00
	OpusOutputPricePerMillion = 25.00

	// Claude Opus 4.1/4.0 pricing (legacy, for historical cost tracking)
	Opus41InputPricePerMillion  = 15.00
	Opus41OutputPricePerMillion = 75.00

	// Claude Haiku 4.5 pricing
	HaikuInputPricePerMillion  = 1.00
	HaikuOutputPricePerMillion = 5.00

	// Aliases for backward compatibility
	Opus46InputPricePerMillion    = OpusInputPricePerMillion
	Opus46OutputPricePerMillion   = OpusOutputPricePerMillion
	Opus45InputPricePerMillion    = OpusInputPricePerMillion  // 4.5 same as 4.6
	Opus45OutputPricePerMillion   = OpusOutputPricePerMillion // 4.5 same as 4.6
	Sonnet35InputPricePerMillion  = SonnetInputPricePerMillion
	Sonnet35OutputPricePerMillion = SonnetOutputPricePerMillion

	// Default model
	DefaultModel = "claude-opus-4-6"
)

// EstimateCost calculates estimated cost from token usage
func EstimateCost(inputTokens, outputTokens int64, model string) float64 {
	var inputPrice, outputPrice float64

	modelLower := strings.ToLower(model)
	switch {
	case strings.Contains(modelLower, "opus-4-1") || strings.Contains(modelLower, "opus-4-0") || model == "claude-opus-4":
		// Legacy Opus 4.1/4.0 pricing
		inputPrice = Opus41InputPricePerMillion
		outputPrice = Opus41OutputPricePerMillion
	case strings.Contains(modelLower, "opus"):
		// Opus 4.6/4.5 pricing (same price: $5/$25)
		inputPrice = OpusInputPricePerMillion
		outputPrice = OpusOutputPricePerMillion
	case strings.Contains(modelLower, "haiku"):
		// Haiku 4.5 pricing
		inputPrice = HaikuInputPricePerMillion
		outputPrice = HaikuOutputPricePerMillion
	default:
		// Sonnet / unknown — default to Sonnet pricing
		inputPrice = SonnetInputPricePerMillion
		outputPrice = SonnetOutputPricePerMillion
	}

	inputCost := float64(inputTokens) * inputPrice / 1_000_000
	outputCost := float64(outputTokens) * outputPrice / 1_000_000
	return inputCost + outputCost
}
