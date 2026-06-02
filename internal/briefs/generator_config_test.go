package briefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

func TestDefaultBriefConfig(t *testing.T) {
	config := DefaultBriefConfig()

	if config.Enabled {
		t.Error("expected disabled by default")
	}

	if config.Schedule != "0 9 * * 1-5" {
		t.Errorf("unexpected schedule: %s", config.Schedule)
	}

	if config.Timezone != "America/New_York" {
		t.Errorf("unexpected timezone: %s", config.Timezone)
	}

	if !config.Content.IncludeMetrics {
		t.Error("expected metrics included by default")
	}

	if !config.Content.IncludeErrors {
		t.Error("expected errors included by default")
	}

	if config.Content.MaxItemsPerSection != 10 {
		t.Errorf("expected max items 10, got %d", config.Content.MaxItemsPerSection)
	}
}

func TestEstimateProgress(t *testing.T) {
	tests := []struct {
		name       string
		durationMs int64
		expected   int
	}{
		{"no duration", 0, 10},
		{"short duration", 30000, 10}, // 30s
		{"mid duration", 150000, 50},  // 2.5min -> 50%
		{"long duration", 600000, 95}, // 10min -> capped at 95%
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &memory.Execution{DurationMs: tt.durationMs}
			result := estimateProgress(exec)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestNewGeneratorWithNilConfig(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Should use default config when nil is passed
	generator := NewGenerator(store, nil)

	if generator == nil {
		t.Fatal("expected generator, got nil")
	}

	if generator.config == nil {
		t.Fatal("expected default config, got nil")
	}

	// Verify default values are set
	if generator.config.Schedule != "0 9 * * 1-5" {
		t.Errorf("expected default schedule, got %s", generator.config.Schedule)
	}
}

func TestConvertMetrics(t *testing.T) {
	data := &memory.BriefMetricsData{
		TotalTasks:     100,
		CompletedCount: 85,
		FailedCount:    15,
		SuccessRate:    0.85,
		AvgDurationMs:  120000,
		PRsCreated:     50,
	}

	metrics := convertMetrics(data)

	if metrics.TotalTasks != 100 {
		t.Errorf("expected TotalTasks 100, got %d", metrics.TotalTasks)
	}
	if metrics.CompletedCount != 85 {
		t.Errorf("expected CompletedCount 85, got %d", metrics.CompletedCount)
	}
	if metrics.FailedCount != 15 {
		t.Errorf("expected FailedCount 15, got %d", metrics.FailedCount)
	}
	if metrics.SuccessRate != 0.85 {
		t.Errorf("expected SuccessRate 0.85, got %f", metrics.SuccessRate)
	}
	if metrics.AvgDurationMs != 120000 {
		t.Errorf("expected AvgDurationMs 120000, got %d", metrics.AvgDurationMs)
	}
	if metrics.PRsCreated != 50 {
		t.Errorf("expected PRsCreated 50, got %d", metrics.PRsCreated)
	}
}

func TestEstimateProgressEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		durationMs int64
		minExpect  int
		maxExpect  int
	}{
		{"exactly at 5 min avg", 300000, 95, 95}, // 5 min = 100%, capped to 95%
		{"just under cap", 280000, 90, 95},
		{"very long running", 900000, 95, 95}, // Well over cap
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &memory.Execution{DurationMs: tt.durationMs}
			result := estimateProgress(exec)
			if result < tt.minExpect || result > tt.maxExpect {
				t.Errorf("expected progress between %d-%d, got %d", tt.minExpect, tt.maxExpect, result)
			}
		})
	}
}

// Integration test to verify the full path works
func TestGeneratorIntegration(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "briefs_integration")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create store
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create generator with full config
	config := &BriefConfig{
		Enabled:  true,
		Schedule: "0 9 * * *",
		Timezone: "UTC",
		Channels: []ChannelConfig{
			{Type: "slack", Channel: "#test"},
		},
		Content: ContentConfig{
			IncludeMetrics:     true,
			IncludeErrors:      true,
			MaxItemsPerSection: 5,
		},
		Filters: FilterConfig{
			Projects: []string{},
		},
	}

	generator := NewGenerator(store, config)

	// Test that empty brief works
	brief, err := generator.GenerateDaily()
	if err != nil {
		t.Fatalf("failed to generate empty brief: %v", err)
	}

	if brief == nil {
		t.Fatal("expected brief, got nil")
	}

	// Verify empty brief has correct structure
	if len(brief.Completed) != 0 {
		t.Errorf("expected 0 completed, got %d", len(brief.Completed))
	}

	if brief.Metrics.TotalTasks != 0 {
		t.Errorf("expected 0 total tasks, got %d", brief.Metrics.TotalTasks)
	}

	// Verify brief can be formatted
	formatter := NewPlainTextFormatter()
	text, err := formatter.Format(brief)
	if err != nil {
		t.Fatalf("failed to format brief: %v", err)
	}

	if text == "" {
		t.Error("expected non-empty formatted text")
	}

	// Verify directory is clean
	files, _ := filepath.Glob(filepath.Join(tmpDir, "*"))
	if len(files) != 1 { // Should only have pilot.db
		t.Logf("warning: unexpected files in temp dir: %v", files)
	}
}
