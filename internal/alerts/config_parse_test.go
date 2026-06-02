package alerts

import (
	"testing"
)

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected Severity
	}{
		{"critical", SeverityCritical},
		{"warning", SeverityWarning},
		{"info", SeverityInfo},
		{"unknown", SeverityWarning},  // Default
		{"", SeverityWarning},         // Default
		{"CRITICAL", SeverityWarning}, // Case sensitive, defaults
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseSeverity(tt.input)
			if result != tt.expected {
				t.Errorf("parseSeverity(%q) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseAlertType(t *testing.T) {
	tests := []struct {
		input    string
		expected AlertType
	}{
		{"task_stuck", AlertTypeTaskStuck},
		{"task_failed", AlertTypeTaskFailed},
		{"consecutive_failures", AlertTypeConsecutiveFails},
		{"service_unhealthy", AlertTypeServiceUnhealthy},
		{"daily_spend_exceeded", AlertTypeDailySpend},
		{"budget_depleted", AlertTypeBudgetDepleted},
		{"usage_spike", AlertTypeUsageSpike},
		{"unauthorized_access", AlertTypeUnauthorizedAccess},
		{"sensitive_file_modified", AlertTypeSensitiveFile},
		{"unusual_pattern", AlertTypeUnusualPattern},
		{"custom_type", AlertType("custom_type")}, // Passthrough for unknown
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseAlertType(tt.input)
			if result != tt.expected {
				t.Errorf("parseAlertType(%q) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}
