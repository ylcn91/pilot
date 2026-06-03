package autopilot

import "testing"

func TestGuardrailsGateConfig_EffectiveMode(t *testing.T) {
	if got := (GuardrailsGateConfig{}).EffectiveMode(); got != guardrailsModeReport {
		t.Errorf("empty mode must resolve to report, got %q", got)
	}
	if got := (GuardrailsGateConfig{Mode: "block"}).EffectiveMode(); got != guardrailsModeBlock {
		t.Errorf("block must resolve to block, got %q", got)
	}
	if got := (GuardrailsGateConfig{Mode: "report"}).EffectiveMode(); got != guardrailsModeReport {
		t.Errorf("explicit report must stay report, got %q", got)
	}
}

func TestGuardrailsGateConfig_BlockingMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  GuardrailsGateConfig
		want bool
	}{
		{"disabled block", GuardrailsGateConfig{Enabled: false, Mode: "block"}, false},
		{"enabled report", GuardrailsGateConfig{Enabled: true, Mode: "report"}, false},
		{"enabled empty", GuardrailsGateConfig{Enabled: true}, false},
		{"enabled block", GuardrailsGateConfig{Enabled: true, Mode: "block"}, true},
		{"disabled empty", GuardrailsGateConfig{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.blockingMode(); got != tc.want {
				t.Errorf("blockingMode() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStatusDescription(t *testing.T) {
	cases := []struct {
		n    int
		mode string
		want string
	}{
		{0, "report", "no architectural guardrail violations"},
		{0, "block", "no architectural guardrail violations"},
		{1, "report", "1 guardrail violation (report-only)"},
		{2, "report", "2 guardrail violations (report-only)"},
		{1, "block", "1 guardrail violation (blocking)"},
		{3, "block", "3 guardrail violations (blocking)"},
	}
	for _, tc := range cases {
		if got := statusDescription(tc.n, tc.mode); got != tc.want {
			t.Errorf("statusDescription(%d,%q) = %q, want %q", tc.n, tc.mode, got, tc.want)
		}
		// Commit-status descriptions must fit GitHub's 140-char ceiling.
		if len(statusDescription(tc.n, tc.mode)) > 140 {
			t.Errorf("status description over 140 chars: %q", statusDescription(tc.n, tc.mode))
		}
	}
}
