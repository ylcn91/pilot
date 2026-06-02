package approval

import (
	"testing"
)

func TestRuleEvaluator_FilePattern_Matches(t *testing.T) {
	rules := []Rule{
		{
			Name:    "infra-guard",
			Enabled: true,
			Stage:   StagePreMerge,
			Condition: Condition{
				Type:    ConditionFilePattern,
				Pattern: "*.tf",
			},
		},
	}

	re := NewRuleEvaluator(rules)

	tests := []struct {
		name    string
		files   []string
		wantNil bool
	}{
		{"no files", nil, true},
		{"no match", []string{"main.go", "readme.md"}, true},
		{"single match", []string{"main.tf"}, false},
		{"match among others", []string{"main.go", "infra.tf", "readme.md"}, false},
		{"multiple matches", []string{"main.tf", "vars.tf"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := RuleContext{
				TaskID:       "TASK-01",
				ChangedFiles: tt.files,
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

func TestRuleEvaluator_FilePattern_EmptyPattern(t *testing.T) {
	rules := []Rule{
		{
			Name:    "empty-pattern",
			Enabled: true,
			Stage:   StagePreMerge,
			Condition: Condition{
				Type:    ConditionFilePattern,
				Pattern: "",
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{ChangedFiles: []string{"anything.go"}})
	if result != nil {
		t.Errorf("expected nil for empty pattern, got rule %q", result.Name)
	}
}

func TestRuleEvaluator_FilePattern_DisabledSkipped(t *testing.T) {
	rules := []Rule{
		{
			Name:    "disabled-file",
			Enabled: false,
			Stage:   StagePreMerge,
			Condition: Condition{
				Type:    ConditionFilePattern,
				Pattern: "*.tf",
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{ChangedFiles: []string{"main.tf"}})
	if result != nil {
		t.Error("disabled rule should not match")
	}
}

func TestRuleEvaluator_FilePattern_GlobPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		file    string
		wantNil bool
	}{
		{"star wildcard", "*.go", "main.go", false},
		{"star no match", "*.go", "main.rs", true},
		{"question mark", "test?.go", "test1.go", false},
		{"question mark no match", "test?.go", "test12.go", true},
		{"character class", "[Mm]akefile", "Makefile", false},
		{"character class lower", "[Mm]akefile", "makefile", false},
		{"character class no match", "[Mm]akefile", "xakefile", true},
		{"exact match", "Dockerfile", "Dockerfile", false},
		{"exact no match", "Dockerfile", "Makefile", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := []Rule{
				{
					Name:    "test-rule",
					Enabled: true,
					Stage:   StagePreMerge,
					Condition: Condition{
						Type:    ConditionFilePattern,
						Pattern: tt.pattern,
					},
				},
			}

			re := NewRuleEvaluator(rules)
			ctx := RuleContext{ChangedFiles: []string{tt.file}}
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

func TestRuleEvaluator_FilePattern_InvalidPattern(t *testing.T) {
	rules := []Rule{
		{
			Name:    "bad-pattern",
			Enabled: true,
			Stage:   StagePreMerge,
			Condition: Condition{
				Type:    ConditionFilePattern,
				Pattern: "[invalid", // Unclosed bracket
			},
		},
	}

	re := NewRuleEvaluator(rules)
	result := re.Evaluate(RuleContext{ChangedFiles: []string{"anything.go"}})
	if result != nil {
		t.Errorf("expected nil for invalid pattern, got rule %q", result.Name)
	}
}
