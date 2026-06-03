package architect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestBugHistoryCollector_Name(t *testing.T) {
	c := NewBugHistoryCollector(nil, memory.MetricsQuery{}, 0, "")
	if c.Name() != "bug_hotspot" {
		t.Fatalf("Name() = %q, want bug_hotspot", c.Name())
	}
}

func TestBugHistoryCollector_NilSourceInert(t *testing.T) {
	c := NewBugHistoryCollector(nil, memory.MetricsQuery{}, 0, "proj")
	got, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("nil source must emit zero signals, got %d", len(got))
	}
}

func TestBugHistoryCollector_QueryErrorDegrades(t *testing.T) {
	src := &mockFailureSource{err: errors.New("db down")}
	c := NewBugHistoryCollector(src, memory.MetricsQuery{}, 0, "proj")
	got, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("query error must not surface, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("query error must yield zero signals, got %d", len(got))
	}
}

func TestBugHistoryCollector_FlagsRecurringNotOneOff(t *testing.T) {
	src := &mockFailureSource{reasons: []*memory.FailureReason{
		{Reason: "panic in runner", Count: 4},                     // recurring => flagged
		{Reason: "transient blip", Count: 1},                      // one-off => excluded
		{Reason: "exactly threshold", Count: bugHistoryThreshold}, // edge => flagged
		nil, // tolerated
	}}
	c := NewBugHistoryCollector(src, memory.MetricsQuery{}, 0, "proj")
	got, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 recurring hotspots, got %d: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Kind != "bug_hotspot" {
			t.Errorf("kind = %q, want bug_hotspot", s.Kind)
		}
		if s.Weight <= 0 {
			t.Errorf("weight must be positive, got %v", s.Weight)
		}
		if !strings.Contains(s.Detail, "recurring failures") {
			t.Errorf("detail missing recurrence context: %q", s.Detail)
		}
	}
}

func TestBugHistoryCollector_WeightScalesWithCount(t *testing.T) {
	src := &mockFailureSource{reasons: []*memory.FailureReason{
		{Reason: "low", Count: 2},
		{Reason: "high", Count: 8},
	}}
	c := NewBugHistoryCollector(src, memory.MetricsQuery{}, 0, "proj")
	got, _ := c.Collect(context.Background(), "/p")
	if len(got) != 2 {
		t.Fatalf("want 2 signals, got %d", len(got))
	}
	byDetail := map[string]float64{}
	for _, s := range got {
		byDetail[s.Detail] = s.Weight
	}
	var low, high float64
	for d, w := range byDetail {
		if strings.Contains(d, "low") {
			low = w
		}
		if strings.Contains(d, "high") {
			high = w
		}
	}
	if !(high > low) {
		t.Fatalf("higher count must weigh more: low=%v high=%v", low, high)
	}
	// count==threshold normalises to 1.0.
	if low != 1.0 {
		t.Errorf("count==threshold weight = %v, want 1.0", low)
	}
}

func TestBugHistoryRisk_Escalation(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  pilotapi.RiskLevel
	}{
		{"threshold medium", bugHistoryThreshold, pilotapi.RiskMedium},
		{"3x high", bugHistoryThreshold * 3, pilotapi.RiskHigh},
		{"5x release blocker", bugHistoryThreshold * 5, pilotapi.RiskReleaseBlocker},
		{"between 3x and 5x stays high", bugHistoryThreshold*4 + 1, pilotapi.RiskHigh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bugHistoryRisk(tt.count, bugHistoryThreshold); got != tt.want {
				t.Errorf("bugHistoryRisk(%d) = %q, want %q", tt.count, got, tt.want)
			}
		})
	}
}

func TestBugHistoryRisk_ZeroThresholdDefaults(t *testing.T) {
	if got := bugHistoryRisk(100, 0); got != pilotapi.RiskMedium {
		t.Errorf("zero threshold must default to medium, got %q", got)
	}
}

func TestBugHistoryWeight_ZeroThreshold(t *testing.T) {
	if got := bugHistoryWeight(7, 0); got != 7 {
		t.Errorf("zero threshold weight = %v, want 7 (raw count)", got)
	}
}

func TestNewBugHistoryCollector_LimitFallback(t *testing.T) {
	c := NewBugHistoryCollector(&mockFailureSource{}, memory.MetricsQuery{}, 0, "")
	if c.limit != defaultBugHistoryLimit {
		t.Errorf("non-positive limit must fall back to %d, got %d", defaultBugHistoryLimit, c.limit)
	}
	c2 := NewBugHistoryCollector(&mockFailureSource{}, memory.MetricsQuery{}, -5, "")
	if c2.limit != defaultBugHistoryLimit {
		t.Errorf("negative limit must fall back to %d, got %d", defaultBugHistoryLimit, c2.limit)
	}
	c3 := NewBugHistoryCollector(&mockFailureSource{}, memory.MetricsQuery{}, 3, "")
	if c3.limit != 3 {
		t.Errorf("explicit limit must be honoured, got %d", c3.limit)
	}
}

func TestBugHistoryCollector_EmptyReasons(t *testing.T) {
	c := NewBugHistoryCollector(&mockFailureSource{reasons: nil}, memory.MetricsQuery{}, 0, "")
	got, err := c.Collect(context.Background(), "/p")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("no reasons => no signals, got %d", len(got))
	}
}
