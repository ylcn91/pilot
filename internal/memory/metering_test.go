package memory

import (
	"testing"
)

func TestMeteringCostCalculations(t *testing.T) {
	t.Run("CalculateTokenCost", func(t *testing.T) {
		tests := []struct {
			name         string
			inputTokens  int64
			outputTokens int64
			wantMin      float64
			wantMax      float64
		}{
			{"zero tokens", 0, 0, 0, 0.001},
			{"small input only", 1000, 0, 0.003, 0.004},
			{"small output only", 0, 1000, 0.017, 0.019},
			{"balanced usage", 10000, 5000, 0.126, 0.128}, // 10K input + 5K output
			{"large usage", 100000, 50000, 1.26, 1.28},    // 100K input + 50K output
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cost := CalculateTokenCost(tt.inputTokens, tt.outputTokens)
				if cost < tt.wantMin || cost > tt.wantMax {
					t.Errorf("CalculateTokenCost(%d, %d) = %f, want between %f and %f",
						tt.inputTokens, tt.outputTokens, cost, tt.wantMin, tt.wantMax)
				}
			})
		}
	})

	t.Run("CalculateComputeCost", func(t *testing.T) {
		tests := []struct {
			name       string
			durationMs int64
			want       float64
		}{
			{"zero duration", 0, 0},
			{"one minute", 60000, 0.01},
			{"five minutes", 300000, 0.05},
			{"one hour", 3600000, 0.60},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cost := CalculateComputeCost(tt.durationMs)
				if cost < tt.want-0.001 || cost > tt.want+0.001 {
					t.Errorf("CalculateComputeCost(%d) = %f, want %f",
						tt.durationMs, cost, tt.want)
				}
			})
		}
	})
}

func TestUsageEventTypes(t *testing.T) {
	// Verify event type constants
	types := []UsageEventType{
		EventTypeTask,
		EventTypeToken,
		EventTypeCompute,
		EventTypeStorage,
		EventTypeAPICall,
	}

	expected := []string{"task", "token", "compute", "storage", "api_call"}

	for i, eventType := range types {
		if string(eventType) != expected[i] {
			t.Errorf("EventType %d = %q, want %q", i, eventType, expected[i])
		}
	}
}

func TestPricingConstants(t *testing.T) {
	// Verify pricing is reasonable
	if PricePerTask <= 0 {
		t.Error("PricePerTask should be positive")
	}

	if TokenInputPricePerMillion <= 0 {
		t.Error("TokenInputPricePerMillion should be positive")
	}

	if TokenOutputPricePerMillion <= TokenInputPricePerMillion {
		t.Error("TokenOutputPricePerMillion should be greater than input price (output is more expensive)")
	}

	if PricePerComputeMinute <= 0 {
		t.Error("PricePerComputeMinute should be positive")
	}
}

func TestCalculateTokenCost_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		inputTokens  int64
		outputTokens int64
		wantZero     bool
	}{
		{"both zero", 0, 0, true},
		{"negative input", -1000, 0, false}, // Should handle gracefully
		{"very large input", 1000000000, 0, false},
		{"very large output", 0, 1000000000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CalculateTokenCost(tt.inputTokens, tt.outputTokens)
			if tt.wantZero && cost != 0 {
				t.Errorf("CalculateTokenCost() = %f, want 0", cost)
			}
		})
	}
}
