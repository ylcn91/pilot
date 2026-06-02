package alerts

import (
	"testing"
)

func TestSeverityConstants(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityCritical, "critical"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			if string(tt.severity) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.severity)
			}
		})
	}
}

func TestAlertTypeConstants(t *testing.T) {
	tests := []struct {
		alertType AlertType
		expected  string
	}{
		{AlertTypeTaskStuck, "task_stuck"},
		{AlertTypeTaskFailed, "task_failed"},
		{AlertTypeConsecutiveFails, "consecutive_failures"},
		{AlertTypeServiceUnhealthy, "service_unhealthy"},
		{AlertTypeDailySpend, "daily_spend_exceeded"},
		{AlertTypeBudgetDepleted, "budget_depleted"},
		{AlertTypeUsageSpike, "usage_spike"},
		{AlertTypeUnauthorizedAccess, "unauthorized_access"},
		{AlertTypeSensitiveFile, "sensitive_file_modified"},
		{AlertTypeUnusualPattern, "unusual_pattern"},
	}

	for _, tt := range tests {
		t.Run(string(tt.alertType), func(t *testing.T) {
			if string(tt.alertType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.alertType)
			}
		})
	}
}
