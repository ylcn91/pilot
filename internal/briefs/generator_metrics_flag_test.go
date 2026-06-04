package briefs

import (
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// TestGenerator_IncludeMetricsFlag verifies that include_metrics gates both
// metric collection in the generator and metric rendering across every
// formatter. When false the metrics section must not appear anywhere.
func TestGenerator_IncludeMetricsFlag(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	execs := []*memory.Execution{
		{
			ID:          "exec-m1",
			TaskID:      "TASK-M1",
			ProjectPath: "/test/project",
			Status:      "completed",
			DurationMs:  120000,
			CreatedAt:   now.Add(-2 * time.Hour),
			CompletedAt: timePtr(now.Add(-1 * time.Hour)),
		},
		{
			ID:          "exec-m2",
			TaskID:      "TASK-M2",
			ProjectPath: "/test/project",
			Status:      "failed",
			Error:       "boom",
			CreatedAt:   now.Add(-3 * time.Hour),
			CompletedAt: timePtr(now.Add(-2 * time.Hour)),
		},
	}
	for _, e := range execs {
		if err := store.SaveExecution(e); err != nil {
			t.Fatalf("save execution: %v", err)
		}
	}

	period := BriefPeriod{Start: now.Add(-24 * time.Hour), End: now}

	tests := []struct {
		name           string
		includeMetrics bool
	}{
		{name: "metrics on", includeMetrics: true},
		{name: "metrics off", includeMetrics: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultBriefConfig()
			config.Content.IncludeMetrics = tt.includeMetrics
			gen := NewGenerator(store, config)

			brief, err := gen.Generate(period)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			if brief.IncludeMetrics != tt.includeMetrics {
				t.Errorf("brief.IncludeMetrics = %v, want %v", brief.IncludeMetrics, tt.includeMetrics)
			}

			if !tt.includeMetrics {
				// Collection skipped → zero-value metrics.
				if brief.Metrics != (BriefMetrics{}) {
					t.Errorf("expected zero metrics when disabled, got %+v", brief.Metrics)
				}
			} else if brief.Metrics.TotalTasks == 0 {
				t.Errorf("expected collected metrics when enabled, got zero")
			}

			plain, err := NewPlainTextFormatter().Format(brief)
			if err != nil {
				t.Fatalf("plain format: %v", err)
			}
			email, err := NewEmailFormatter().Format(brief)
			if err != nil {
				t.Fatalf("email format: %v", err)
			}
			slack, err := NewSlackFormatter().Format(brief)
			if err != nil {
				t.Fatalf("slack format: %v", err)
			}
			blocks := NewSlackFormatter().SlackBlocks(brief)
			blocksHaveMetrics := false
			for _, b := range blocks {
				if el, ok := b["elements"].([]map[string]interface{}); ok {
					for _, e := range el {
						if txt, ok := e["text"].(string); ok && strings.Contains(txt, "Metrics") {
							blocksHaveMetrics = true
						}
					}
				}
			}

			wantMetrics := tt.includeMetrics
			// "Success rate" only appears in rendered metrics content, never in
			// boilerplate/CSS, so it is a reliable presence marker.
			assertMetrics := func(label, out string) {
				has := strings.Contains(out, "Success rate")
				if has != wantMetrics {
					t.Errorf("%s: metrics present=%v, want %v\n%s", label, has, wantMetrics, out)
				}
			}
			assertMetrics("plain", plain)
			assertMetrics("email", email)
			assertMetrics("slack", slack)
			if blocksHaveMetrics != wantMetrics {
				t.Errorf("slack blocks: metrics present=%v, want %v", blocksHaveMetrics, wantMetrics)
			}
		})
	}
}
