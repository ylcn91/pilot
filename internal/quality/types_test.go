package quality

import (
	"testing"
	"time"
)

func TestGate_DefaultTimeout(t *testing.T) {
	tests := []struct {
		name     string
		gate     *Gate
		expected time.Duration
	}{
		{
			name:     "custom timeout",
			gate:     &Gate{Type: GateBuild, Timeout: 10 * time.Minute},
			expected: 10 * time.Minute,
		},
		{
			name:     "build default",
			gate:     &Gate{Type: GateBuild},
			expected: 5 * time.Minute,
		},
		{
			name:     "test default",
			gate:     &Gate{Type: GateTest},
			expected: 10 * time.Minute,
		},
		{
			name:     "lint default",
			gate:     &Gate{Type: GateLint},
			expected: 2 * time.Minute,
		},
		{
			name:     "coverage default",
			gate:     &Gate{Type: GateCoverage},
			expected: 10 * time.Minute,
		},
		{
			name:     "security default",
			gate:     &Gate{Type: GateSecurity},
			expected: 5 * time.Minute,
		},
		{
			name:     "typecheck default",
			gate:     &Gate{Type: GateTypeCheck},
			expected: 3 * time.Minute,
		},
		{
			name:     "custom gate default",
			gate:     &Gate{Type: GateCustom},
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gate.DefaultTimeout()
			if got != tt.expected {
				t.Errorf("DefaultTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestResult_Passed(t *testing.T) {
	tests := []struct {
		name     string
		status   GateStatus
		expected bool
	}{
		{"passed", StatusPassed, true},
		{"failed", StatusFailed, false},
		{"pending", StatusPending, false},
		{"running", StatusRunning, false},
		{"skipped", StatusSkipped, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{Status: tt.status}
			if got := r.Passed(); got != tt.expected {
				t.Errorf("Passed() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCheckResults_GetFailedGates(t *testing.T) {
	results := &CheckResults{
		Results: []*Result{
			{GateName: "build", Status: StatusPassed},
			{GateName: "test", Status: StatusFailed},
			{GateName: "lint", Status: StatusFailed},
			{GateName: "coverage", Status: StatusSkipped},
		},
	}

	failed := results.GetFailedGates()
	if len(failed) != 2 {
		t.Errorf("expected 2 failed gates, got %d", len(failed))
	}

	names := make(map[string]bool)
	for _, f := range failed {
		names[f.GateName] = true
	}
	if !names["test"] || !names["lint"] {
		t.Error("expected test and lint to be in failed gates")
	}
}
