package linear

import (
	"testing"
)

func TestPriorityName(t *testing.T) {
	tests := []struct {
		priority int
		want     string
	}{
		{PriorityNone, "No Priority"},
		{PriorityUrgent, "Urgent"},
		{PriorityHigh, "High"},
		{PriorityMedium, "Medium"},
		{PriorityLow, "Low"},
		{-1, "No Priority"},  // Unknown negative
		{5, "No Priority"},   // Unknown positive
		{100, "No Priority"}, // Out of range
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := PriorityName(tt.priority)
			if got != tt.want {
				t.Errorf("PriorityName(%d) = %s, want %s", tt.priority, got, tt.want)
			}
		})
	}
}

func TestPriorityConstants(t *testing.T) {
	tests := []struct {
		name     string
		priority int
		want     int
	}{
		{"PriorityNone", PriorityNone, 0},
		{"PriorityUrgent", PriorityUrgent, 1},
		{"PriorityHigh", PriorityHigh, 2},
		{"PriorityMedium", PriorityMedium, 3},
		{"PriorityLow", PriorityLow, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.priority != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.priority, tt.want)
			}
		})
	}
}

func TestStateTypeConstants(t *testing.T) {
	tests := []struct {
		stateType StateType
		want      string
	}{
		{StateTypeBacklog, "backlog"},
		{StateTypeUnstarted, "unstarted"},
		{StateTypeStarted, "started"},
		{StateTypeCompleted, "completed"},
		{StateTypeCanceled, "canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.stateType) != tt.want {
				t.Errorf("StateType = %s, want %s", tt.stateType, tt.want)
			}
		})
	}
}

func TestStateTypeComparison(t *testing.T) {
	// Verify StateType can be used in comparisons
	currentState := StateTypeStarted

	if currentState != StateTypeStarted {
		t.Error("state comparison failed")
	}

	if currentState == StateTypeCompleted {
		t.Error("state should not equal completed")
	}

	// Verify string conversion works
	if string(currentState) != "started" {
		t.Errorf("string(StateTypeStarted) = %s, want 'started'", string(currentState))
	}
}

func TestPriorityOrdering(t *testing.T) {
	// Verify priority ordering: Urgent > High > Medium > Low > None
	// In Linear, lower number = higher priority
	if PriorityUrgent >= PriorityHigh {
		t.Error("Urgent should have lower value than High")
	}
	if PriorityHigh >= PriorityMedium {
		t.Error("High should have lower value than Medium")
	}
	if PriorityMedium >= PriorityLow {
		t.Error("Medium should have lower value than Low")
	}
	// PriorityNone (0) is special - means no priority set
	if PriorityNone != 0 {
		t.Error("PriorityNone should be 0")
	}
}
