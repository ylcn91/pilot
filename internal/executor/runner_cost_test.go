package executor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name         string
		inputTokens  int64
		outputTokens int64
		model        string
		minCost      float64
		maxCost      float64
	}{
		{
			name:         "sonnet zero tokens",
			inputTokens:  0,
			outputTokens: 0,
			model:        "claude-sonnet-4-6",
			minCost:      0,
			maxCost:      0,
		},
		{
			name:         "sonnet 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "claude-sonnet-4-6",
			minCost:      2.9,
			maxCost:      3.1,
		},
		{
			name:         "sonnet 1M output tokens",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "claude-sonnet-4-6",
			minCost:      14.9,
			maxCost:      15.1,
		},
		{
			name:         "opus 4.6 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "claude-opus-4-6",
			minCost:      4.9,
			maxCost:      5.1,
		},
		{
			name:         "opus 4.6 1M output tokens",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "claude-opus-4-6",
			minCost:      24.9,
			maxCost:      25.1,
		},
		{
			name:         "opus 4.5 1M input tokens (same as 4.6)",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "claude-opus-4-6",
			minCost:      4.9,
			maxCost:      5.1,
		},
		{
			name:         "opus 4.5 1M output tokens (same as 4.6)",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "claude-opus-4-6",
			minCost:      24.9,
			maxCost:      25.1,
		},
		{
			name:         "legacy opus 4.1 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "claude-opus-4-1-20250805",
			minCost:      14.9,
			maxCost:      15.1,
		},
		{
			name:         "legacy opus 4.1 1M output tokens",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "claude-opus-4-1-20250805",
			minCost:      74.9,
			maxCost:      75.1,
		},
		{
			name:         "mixed usage sonnet",
			inputTokens:  100000,
			outputTokens: 50000,
			model:        "claude-sonnet-4-6",
			minCost:      1.0,
			maxCost:      1.1, // 0.3 + 0.75
		},
		{
			name:         "case insensitive opus uses 4.6 pricing",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "Claude-OPUS-4-6",
			minCost:      4.9,
			maxCost:      5.1,
		},
		{
			name:         "haiku 4.5 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "claude-haiku-4-5-20251001",
			minCost:      0.9,
			maxCost:      1.1,
		},
		{
			name:         "haiku 4.5 1M output tokens",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "claude-haiku-4-5-20251001",
			minCost:      4.9,
			maxCost:      5.1,
		},
		{
			name:         "qwen coder-next 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "qwen3-coder-next",
			minCost:      0.06,
			maxCost:      0.08,
		},
		{
			name:         "qwen coder-next 1M output tokens",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "qwen3-coder-next",
			minCost:      0.29,
			maxCost:      0.31,
		},
		{
			name:         "qwen coder-480b 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "qwen3-coder-480b",
			minCost:      0.99,
			maxCost:      1.01,
		},
		{
			name:         "qwen coder-plus 1M output tokens",
			inputTokens:  0,
			outputTokens: 1000000,
			model:        "qwen3-coder-plus",
			minCost:      4.99,
			maxCost:      5.01,
		},
		{
			name:         "qwen coder-flash 1M input tokens",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "qwen3-coder-flash",
			minCost:      0.29,
			maxCost:      0.31,
		},
		{
			name:         "qwen generic model uses coder-next pricing",
			inputTokens:  1000000,
			outputTokens: 0,
			model:        "qwen-max",
			minCost:      0.06,
			maxCost:      0.08,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := estimateCost(tt.inputTokens, tt.outputTokens, tt.model)
			if cost < tt.minCost || cost > tt.maxCost {
				t.Errorf("estimateCost() = %f, want between %f and %f", cost, tt.minCost, tt.maxCost)
			}
		})
	}
}

func TestEstimateCostWithCache(t *testing.T) {
	tests := []struct {
		name          string
		input         int64
		output        int64
		cacheCreation int64
		cacheRead     int64
		model         string
		minCost       float64
		maxCost       float64
	}{
		{
			name:          "no cache tokens matches estimateCost",
			input:         1000000,
			output:        100000,
			cacheCreation: 0,
			cacheRead:     0,
			model:         "claude-sonnet-4-6",
			minCost:       4.49, // 3.0 + 1.5
			maxCost:       4.51,
		},
		{
			name:          "sonnet cache creation 1M tokens at 125% of input price",
			input:         0,
			output:        0,
			cacheCreation: 1000000,
			cacheRead:     0,
			model:         "claude-sonnet-4-6",
			minCost:       3.74, // 3.00 * 1.25 = 3.75
			maxCost:       3.76,
		},
		{
			name:          "sonnet cache read 1M tokens at 10% of input price",
			input:         0,
			output:        0,
			cacheCreation: 0,
			cacheRead:     1000000,
			model:         "claude-sonnet-4-6",
			minCost:       0.29, // 3.00 * 0.10 = 0.30
			maxCost:       0.31,
		},
		{
			name:          "opus cache creation 1M tokens",
			input:         0,
			output:        0,
			cacheCreation: 1000000,
			cacheRead:     0,
			model:         "claude-opus-4-6",
			minCost:       6.24, // 5.00 * 1.25 = 6.25
			maxCost:       6.26,
		},
		{
			name:          "opus cache read 1M tokens",
			input:         0,
			output:        0,
			cacheCreation: 0,
			cacheRead:     1000000,
			model:         "claude-opus-4-6",
			minCost:       0.49, // 5.00 * 0.10 = 0.50
			maxCost:       0.51,
		},
		{
			name:          "mixed usage with cache - realistic scenario",
			input:         50000,  // 50K regular input
			output:        20000,  // 20K output
			cacheCreation: 100000, // 100K cache write
			cacheRead:     800000, // 800K cache read (typical with 1M context)
			model:         "claude-sonnet-4-6",
			// input: 50000 * 3.00 / 1M = 0.15
			// output: 20000 * 15.00 / 1M = 0.30
			// cache_create: 100000 * 3.75 / 1M = 0.375
			// cache_read: 800000 * 0.30 / 1M = 0.24
			minCost: 1.06,
			maxCost: 1.07,
		},
		{
			name:          "backward compat: estimateCost equals estimateCostWithCache(0,0)",
			input:         500000,
			output:        100000,
			cacheCreation: 0,
			cacheRead:     0,
			model:         "claude-opus-4-6",
			minCost:       4.99, // 500K * 5/1M + 100K * 25/1M = 2.5 + 2.5
			maxCost:       5.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := estimateCostWithCache(tt.input, tt.output, tt.cacheCreation, tt.cacheRead, tt.model)
			if cost < tt.minCost || cost > tt.maxCost {
				t.Errorf("estimateCostWithCache() = %f, want between %f and %f", cost, tt.minCost, tt.maxCost)
			}
		})
	}

	// Verify backward compatibility: estimateCost == estimateCostWithCache with zero cache
	t.Run("backward_compat_wrapper", func(t *testing.T) {
		old := estimateCost(1000000, 500000, "claude-opus-4-6")
		new := estimateCostWithCache(1000000, 500000, 0, 0, "claude-opus-4-6")
		if old != new {
			t.Errorf("estimateCost(%f) != estimateCostWithCache(%f) — backward compat broken", old, new)
		}
	})
}

func TestUsageInfoCacheFields(t *testing.T) {
	// Verify UsageInfo correctly unmarshals cache token fields from stream-json
	jsonStr := `{"input_tokens": 1000, "output_tokens": 500, "cache_creation_input_tokens": 200, "cache_read_input_tokens": 800}`
	var usage UsageInfo
	if err := json.Unmarshal([]byte(jsonStr), &usage); err != nil {
		t.Fatalf("Failed to unmarshal UsageInfo: %v", err)
	}
	if usage.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", usage.InputTokens)
	}
	if usage.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want 500", usage.OutputTokens)
	}
	if usage.CacheCreationInputTokens != 200 {
		t.Errorf("CacheCreationInputTokens = %d, want 200", usage.CacheCreationInputTokens)
	}
	if usage.CacheReadInputTokens != 800 {
		t.Errorf("CacheReadInputTokens = %d, want 800", usage.CacheReadInputTokens)
	}

	// Verify omitempty: zero cache fields should not appear in JSON
	usage2 := UsageInfo{InputTokens: 100, OutputTokens: 50}
	data, err := json.Marshal(usage2)
	if err != nil {
		t.Fatalf("Failed to marshal UsageInfo: %v", err)
	}
	str := string(data)
	if strings.Contains(str, "cache_creation") {
		t.Errorf("Zero cache_creation should be omitted, got: %s", str)
	}
	if strings.Contains(str, "cache_read") {
		t.Errorf("Zero cache_read should be omitted, got: %s", str)
	}
}
