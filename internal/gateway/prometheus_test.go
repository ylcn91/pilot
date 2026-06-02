package gateway

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/autopilot"
)

// mockMetricsSource implements MetricsSource for testing.
type mockMetricsSource struct {
	snapshot          autopilot.MetricsSnapshot
	histogramSnapshot autopilot.HistogramData
}

func (m *mockMetricsSource) Snapshot() autopilot.MetricsSnapshot {
	return m.snapshot
}

func (m *mockMetricsSource) HistogramSnapshot() autopilot.HistogramData {
	return m.histogramSnapshot
}

func TestPrometheusExporter_WritePrometheus(t *testing.T) {
	tests := []struct {
		name     string
		source   *mockMetricsSource
		contains []string
	}{
		{
			name: "empty metrics",
			source: &mockMetricsSource{
				snapshot: autopilot.MetricsSnapshot{
					IssuesProcessed:  make(map[string]int64),
					APIErrors:        make(map[string]int64),
					LabelCleanups:    make(map[string]int64),
					ActivePRsByStage: make(map[autopilot.PRStage]int),
				},
				histogramSnapshot: autopilot.HistogramData{},
			},
			contains: []string{
				"# HELP pilot_issues_processed_total",
				"# TYPE pilot_issues_processed_total counter",
				`pilot_issues_processed_total{result="success"} 0`,
				`pilot_issues_processed_total{result="failed"} 0`,
				"# HELP pilot_prs_merged_total",
				"pilot_prs_merged_total 0",
				"# HELP pilot_queue_depth",
				"pilot_queue_depth 0",
				"# HELP pilot_pr_time_to_merge_seconds",
				"# TYPE pilot_pr_time_to_merge_seconds histogram",
				`pilot_pr_time_to_merge_seconds_bucket{le="+Inf"} 0`,
				"pilot_pr_time_to_merge_seconds_sum 0",
				"pilot_pr_time_to_merge_seconds_count 0",
			},
		},
		{
			name: "populated counters",
			source: &mockMetricsSource{
				snapshot: autopilot.MetricsSnapshot{
					IssuesProcessed: map[string]int64{
						"success": 42,
						"failed":  5,
					},
					PRsMerged:           35,
					PRsFailed:           3,
					PRsConflicting:      2,
					CircuitBreakerTrips: 1,
					APIErrors: map[string]int64{
						"GetPR":   10,
						"MergePR": 2,
					},
					LabelCleanups: map[string]int64{
						"pilot-in-progress": 8,
					},
					ActivePRsByStage: make(map[autopilot.PRStage]int),
				},
				histogramSnapshot: autopilot.HistogramData{},
			},
			contains: []string{
				`pilot_issues_processed_total{result="success"} 42`,
				`pilot_issues_processed_total{result="failed"} 5`,
				"pilot_prs_merged_total 35",
				"pilot_prs_failed_total 3",
				"pilot_prs_conflicting_total 2",
				"pilot_circuit_breaker_trips_total 1",
				`pilot_api_errors_total{endpoint="GetPR"} 10`,
				`pilot_api_errors_total{endpoint="MergePR"} 2`,
				`pilot_label_cleanups_total{label="pilot-in-progress"} 8`,
			},
		},
		{
			name: "populated gauges",
			source: &mockMetricsSource{
				snapshot: autopilot.MetricsSnapshot{
					IssuesProcessed: make(map[string]int64),
					APIErrors:       make(map[string]int64),
					LabelCleanups:   make(map[string]int64),
					ActivePRsByStage: map[autopilot.PRStage]int{
						autopilot.StageWaitingCI: 3,
						autopilot.StageMerging:   1,
					},
					TotalActivePRs:   4,
					QueueDepth:       7,
					FailedQueueDepth: 2,
					APIErrorRate:     1.5,
					SuccessRate:      0.85,
				},
				histogramSnapshot: autopilot.HistogramData{},
			},
			contains: []string{
				"pilot_queue_depth 7",
				"pilot_failed_queue_depth 2",
				`pilot_active_prs{stage="waiting_ci"} 3`,
				`pilot_active_prs{stage="merging"} 1`,
				"pilot_active_prs_total 4",
				"pilot_api_error_rate 1.5",
				"pilot_success_rate 0.85",
			},
		},
		{
			name: "absent stages emitted as zero",
			source: &mockMetricsSource{
				snapshot: autopilot.MetricsSnapshot{
					IssuesProcessed: make(map[string]int64),
					APIErrors:       make(map[string]int64),
					LabelCleanups:   make(map[string]int64),
					ActivePRsByStage: map[autopilot.PRStage]int{
						autopilot.StageCIPassed: 1,
					},
				},
				histogramSnapshot: autopilot.HistogramData{},
			},
			contains: []string{
				`pilot_active_prs{stage="ci_passed"} 1`,
				`pilot_active_prs{stage="waiting_ci"} 0`,
				`pilot_active_prs{stage="merging"} 0`,
				`pilot_active_prs{stage="merged"} 0`,
			},
		},
		{
			name: "histogram with samples",
			source: &mockMetricsSource{
				snapshot: autopilot.MetricsSnapshot{
					IssuesProcessed:  make(map[string]int64),
					APIErrors:        make(map[string]int64),
					LabelCleanups:    make(map[string]int64),
					ActivePRsByStage: make(map[autopilot.PRStage]int),
				},
				histogramSnapshot: autopilot.HistogramData{
					PRTimeToMerge: []time.Duration{
						30 * time.Second,  // in 60s bucket
						90 * time.Second,  // in 300s bucket
						400 * time.Second, // in 600s bucket
						700 * time.Second, // in 1800s bucket
					},
					ExecutionDurations: []time.Duration{
						5 * time.Second,
						25 * time.Second,
						45 * time.Second,
					},
					CIWaitDurations: []time.Duration{
						120 * time.Second,
					},
				},
			},
			contains: []string{
				// PR time to merge histogram
				`pilot_pr_time_to_merge_seconds_bucket{le="60"} 1`,
				`pilot_pr_time_to_merge_seconds_bucket{le="300"} 2`,
				`pilot_pr_time_to_merge_seconds_bucket{le="600"} 3`,
				`pilot_pr_time_to_merge_seconds_bucket{le="1800"} 4`,
				`pilot_pr_time_to_merge_seconds_bucket{le="+Inf"} 4`,
				"pilot_pr_time_to_merge_seconds_count 4",
				// Execution duration histogram
				`pilot_execution_duration_seconds_bucket{le="10"} 1`,
				`pilot_execution_duration_seconds_bucket{le="30"} 2`,
				`pilot_execution_duration_seconds_bucket{le="60"} 3`,
				"pilot_execution_duration_seconds_count 3",
				// CI wait histogram
				"pilot_ci_wait_duration_seconds_count 1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := NewPrometheusExporter(tt.source)
			var buf bytes.Buffer

			err := exporter.WritePrometheus(&buf)
			if err != nil {
				t.Fatalf("WritePrometheus() error = %v", err)
			}

			output := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("Output missing expected string: %q\nGot:\n%s", want, output)
				}
			}
		})
	}
}

func TestPrometheusExporter_TokenCostExecutionMetrics(t *testing.T) {
	m := autopilot.NewMetrics()
	m.RecordTokens("claude-sonnet-4-5", "input", 1000)
	m.RecordTokens("claude-sonnet-4-5", "output", 250)
	m.RecordCost("claude-sonnet-4-5", 0.005)
	m.RecordExecution("claude-sonnet-4-5", "success")
	m.RecordExecution("claude-sonnet-4-5", "failed")

	exporter := NewPrometheusExporter(m)
	var buf bytes.Buffer
	if err := exporter.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	output := buf.String()

	for _, want := range []string{
		"# HELP pilot_tokens_consumed_total Total tokens consumed by model and direction",
		"# TYPE pilot_tokens_consumed_total counter",
		`pilot_tokens_consumed_total{model="claude-sonnet-4-5",direction="input"} 1000`,
		`pilot_tokens_consumed_total{model="claude-sonnet-4-5",direction="output"} 250`,
		"# HELP pilot_execution_cost_usd_total Total execution cost in USD by model",
		"# TYPE pilot_execution_cost_usd_total counter",
		`pilot_execution_cost_usd_total{model="claude-sonnet-4-5"} 0.005`,
		"# HELP pilot_executions_total Total executions by model and result",
		"# TYPE pilot_executions_total counter",
		`pilot_executions_total{model="claude-sonnet-4-5",result="success"} 1`,
		`pilot_executions_total{model="claude-sonnet-4-5",result="failed"} 1`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("Output missing expected string: %q\nGot:\n%s", want, output)
		}
	}
}

// TestPrometheusExporter_IssuesProcessedCounter verifies that a real *autopilot.Metrics
// wired to the exporter surfaces pilot_issues_processed_total, pilot_execution_duration_seconds_count,
// and pilot_success_rate with correct non-zero values after recording one success + one duration.
func TestPrometheusExporter_IssuesProcessedCounter(t *testing.T) {
	m := autopilot.NewMetrics()
	m.RecordIssueProcessed("success")
	m.RecordExecutionDuration(30 * time.Second)

	exporter := NewPrometheusExporter(m)
	var buf bytes.Buffer
	if err := exporter.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	output := buf.String()

	for _, want := range []string{
		`pilot_issues_processed_total{result="success"} 1`,
		"pilot_execution_duration_seconds_count 1",
		"pilot_success_rate 1",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("Output missing expected string: %q\nGot:\n%s", want, output)
		}
	}
}
