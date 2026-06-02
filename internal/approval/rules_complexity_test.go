package approval

import (
	"testing"
)

func TestRuleEvaluator_Complexity_Matches(t *testing.T) {
	rules := []Rule{
		{
			Name:    "complex-gate",
			Enabled: true,
			Stage:   StagePreExecution,
			Condition: Condition{
				Type:    ConditionComplexity,
				Pattern: "complex", // Trigger at complex or above
			},
		},
	}

	re := NewRuleEvaluator(rules)

	tests := []struct {
		name       string
		complexity string
		wantNil    bool
	}{
		{"empty complexity", "", true},
		{"trivial below threshold", "trivial", true},
		{"simple below threshold", "simple", true},
		{"medium below threshold", "medium", true},
		{"complex at threshold", "complex", false},
		{"epic above threshold", "epic", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := RuleContext{
				TaskID:     "TASK-01",
				Complexity: tt.complexity,
			}
			result := re.Evaluate(ctx)
			if tt.wantNil && result != nil {
				t.Errorf("expected nil, got rule %q", result.Name)
			}
			if !tt.wantNil && result == nil {
				t.Error("expected matching rule, got nil")
			}
		})
	}
}

func TestRuleEvaluator_Complexity_AllLevels(t *testing.T) {
	// Test each level as threshold — everything at or above matches
	levels := []string{"trivial", "simple", "medium", "complex", "epic"}

	for i, threshold := range levels {
		rules := []Rule{
			{
				Name:    "level-gate",
				Enabled: true,
				Stage:   StagePreExecution,
				Condition: Condition{
					Type:    ConditionComplexity,
					Pattern: threshold,
				},
			},
		}

		re := NewRuleEvaluator(rules)

		for j, actual := range levels {
			ctx := RuleContext{TaskID: "TASK-01", Complexity: actual}
			result := re.Evaluate(ctx)
			shouldMatch := j >= i

			if shouldMatch && result == nil {
				t.Errorf("threshold=%s actual=%s: expected match, got nil", threshold, actual)
			}
			if !shouldMatch && result != nil {
				t.Errorf("threshold=%s actual=%s: expected nil, got match", threshold, actual)
			}
		}
	}
}

func TestRuleEvaluator_Complexity_EmptyPattern(t *testing.T) {
	rules := []Rule{
		{
			Name:    "empty-complexity",
			Enabled: true,
			Stage:   StagePreExecution,
			Condition: Condition{
				Type:    ConditionComplexity,
				Pattern: "",
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{Complexity: "complex"})
	if result != nil {
		t.Errorf("expected nil for empty pattern, got rule %q", result.Name)
	}
}

func TestRuleEvaluator_Complexity_UnknownThreshold(t *testing.T) {
	rules := []Rule{
		{
			Name:    "bad-threshold",
			Enabled: true,
			Stage:   StagePreExecution,
			Condition: Condition{
				Type:    ConditionComplexity,
				Pattern: "impossible",
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{Complexity: "complex"})
	if result != nil {
		t.Errorf("expected nil for unknown threshold, got rule %q", result.Name)
	}
}

func TestRuleEvaluator_Complexity_UnknownActual(t *testing.T) {
	rules := []Rule{
		{
			Name:    "valid-threshold",
			Enabled: true,
			Stage:   StagePreExecution,
			Condition: Condition{
				Type:    ConditionComplexity,
				Pattern: "medium",
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{Complexity: "unknown_level"})
	if result != nil {
		t.Errorf("expected nil for unknown actual complexity, got rule %q", result.Name)
	}
}

func TestRuleEvaluator_Complexity_DisabledSkipped(t *testing.T) {
	rules := []Rule{
		{
			Name:    "disabled-complexity",
			Enabled: false,
			Stage:   StagePreExecution,
			Condition: Condition{
				Type:    ConditionComplexity,
				Pattern: "trivial",
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{Complexity: "epic"})
	if result != nil {
		t.Error("disabled rule should not match")
	}
}
