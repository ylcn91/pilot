package memory

import (
	"encoding/json"
	"fmt"
	"time"
)

// UsageEventType represents the type of billable event
type UsageEventType string

const (
	EventTypeTask    UsageEventType = "task"     // Task execution
	EventTypeToken   UsageEventType = "token"    // Claude API tokens
	EventTypeCompute UsageEventType = "compute"  // Execution time (minutes)
	EventTypeStorage UsageEventType = "storage"  // Memory/logs storage
	EventTypeAPICall UsageEventType = "api_call" // External API calls
)

// UsageEvent represents a single billable event
type UsageEvent struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	UserID      string                 `json:"user_id"`
	ProjectID   string                 `json:"project_id"`
	EventType   UsageEventType         `json:"event_type"`
	Quantity    int64                  `json:"quantity"`
	UnitCost    float64                `json:"unit_cost"`
	TotalCost   float64                `json:"total_cost"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	ExecutionID string                 `json:"execution_id,omitempty"` // Link to execution
}

// UsageSummary holds aggregated usage for a period
type UsageSummary struct {
	UserID      string    `json:"user_id"`
	ProjectID   string    `json:"project_id,omitempty"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`

	// Task metrics
	TaskCount int64   `json:"task_count"`
	TaskCost  float64 `json:"task_cost"`

	// Token metrics
	TokensInput  int64   `json:"tokens_input"`
	TokensOutput int64   `json:"tokens_output"`
	TokensTotal  int64   `json:"tokens_total"`
	TokenCost    float64 `json:"token_cost"`

	// Compute metrics
	ComputeMinutes int64   `json:"compute_minutes"`
	ComputeCost    float64 `json:"compute_cost"`

	// Storage metrics
	StorageBytes int64   `json:"storage_bytes"`
	StorageCost  float64 `json:"storage_cost"`

	// API call metrics
	APICallCount int64   `json:"api_call_count"`
	APICallCost  float64 `json:"api_call_cost"`

	// Totals
	TotalCost float64 `json:"total_cost"`
}

// UsageQuery holds parameters for querying usage
type UsageQuery struct {
	UserID    string
	ProjectID string
	Start     time.Time
	End       time.Time
	EventType UsageEventType // Empty = all types
}

// Pricing constants (can be made configurable)
const (
	// Per task flat fee
	PricePerTask = 1.00 // $1.00 per task execution

	// Token pricing with 20% margin
	TokenInputPricePerMillion  = 3.60  // $3.00 + 20%
	TokenOutputPricePerMillion = 18.00 // $15.00 + 20%

	// Compute pricing
	PricePerComputeMinute = 0.01 // $0.01 per minute

	// Storage pricing (per GB per month)
	PricePerGBMonth = 0.10

	// API call pricing
	PricePerAPICall = 0.001 // $0.001 per call
)

// CalculateTokenCost calculates cost for token usage with margin
func CalculateTokenCost(inputTokens, outputTokens int64) float64 {
	inputCost := float64(inputTokens) * TokenInputPricePerMillion / 1_000_000
	outputCost := float64(outputTokens) * TokenOutputPricePerMillion / 1_000_000
	return inputCost + outputCost
}

// CalculateComputeCost calculates cost for compute time
func CalculateComputeCost(durationMs int64) float64 {
	minutes := float64(durationMs) / 60000.0
	return minutes * PricePerComputeMinute
}

// RecordUsageEvent saves a usage event
func (s *Store) RecordUsageEvent(event *UsageEvent) error {
	metadata, _ := json.Marshal(event.Metadata)

	_, err := s.db.Exec(`
		INSERT INTO usage_events (id, timestamp, user_id, project_id, event_type, quantity, unit_cost, total_cost, metadata, execution_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.Timestamp, event.UserID, event.ProjectID, event.EventType, event.Quantity, event.UnitCost, event.TotalCost, string(metadata), event.ExecutionID)
	return err
}

// RecordTaskUsage records usage for a completed task
func (s *Store) RecordTaskUsage(executionID, userID, projectID string, durationMs, tokensInput, tokensOutput int64) error {
	now := time.Now()

	// Record task event
	taskEvent := &UsageEvent{
		ID:          fmt.Sprintf("evt_%s_task", executionID),
		Timestamp:   now,
		UserID:      userID,
		ProjectID:   projectID,
		EventType:   EventTypeTask,
		Quantity:    1,
		UnitCost:    PricePerTask,
		TotalCost:   PricePerTask,
		ExecutionID: executionID,
	}
	if err := s.RecordUsageEvent(taskEvent); err != nil {
		return fmt.Errorf("failed to record task event: %w", err)
	}

	// Record token event
	if tokensInput > 0 || tokensOutput > 0 {
		tokenCost := CalculateTokenCost(tokensInput, tokensOutput)
		tokenEvent := &UsageEvent{
			ID:          fmt.Sprintf("evt_%s_token", executionID),
			Timestamp:   now,
			UserID:      userID,
			ProjectID:   projectID,
			EventType:   EventTypeToken,
			Quantity:    tokensInput + tokensOutput,
			UnitCost:    tokenCost / float64(tokensInput+tokensOutput),
			TotalCost:   tokenCost,
			ExecutionID: executionID,
			Metadata: map[string]interface{}{
				"input_tokens":  tokensInput,
				"output_tokens": tokensOutput,
			},
		}
		if err := s.RecordUsageEvent(tokenEvent); err != nil {
			return fmt.Errorf("failed to record token event: %w", err)
		}
	}

	// Record compute event
	if durationMs > 0 {
		computeCost := CalculateComputeCost(durationMs)
		computeEvent := &UsageEvent{
			ID:          fmt.Sprintf("evt_%s_compute", executionID),
			Timestamp:   now,
			UserID:      userID,
			ProjectID:   projectID,
			EventType:   EventTypeCompute,
			Quantity:    durationMs / 60000, // minutes
			UnitCost:    PricePerComputeMinute,
			TotalCost:   computeCost,
			ExecutionID: executionID,
			Metadata: map[string]interface{}{
				"duration_ms": durationMs,
			},
		}
		if err := s.RecordUsageEvent(computeEvent); err != nil {
			return fmt.Errorf("failed to record compute event: %w", err)
		}
	}

	return nil
}
