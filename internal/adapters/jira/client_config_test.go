package jira

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled != false {
		t.Errorf("default Enabled = %v, want false", cfg.Enabled)
	}
	if cfg.Platform != "cloud" {
		t.Errorf("default Platform = %s, want 'cloud'", cfg.Platform)
	}
	if cfg.PilotLabel != "pilot" {
		t.Errorf("default PilotLabel = %s, want 'pilot'", cfg.PilotLabel)
	}
}

func TestPriorityFromJira(t *testing.T) {
	tests := []struct {
		name string
		want Priority
	}{
		{"Highest", PriorityHighest},
		{"Blocker", PriorityHighest},
		{"Critical", PriorityHighest},
		{"High", PriorityHigh},
		{"Major", PriorityHigh},
		{"Medium", PriorityMedium},
		{"Low", PriorityLow},
		{"Minor", PriorityLow},
		{"Lowest", PriorityLowest},
		{"Trivial", PriorityLowest},
		{"Unknown", PriorityNone},
		{"", PriorityNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PriorityFromJira(tt.name)
			if got != tt.want {
				t.Errorf("PriorityFromJira(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}
