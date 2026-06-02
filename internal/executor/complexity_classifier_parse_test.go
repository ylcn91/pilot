package executor

import (
	"testing"
)

func TestNewComplexityClassifier(t *testing.T) {
	// No API key needed anymore - uses Claude Code subscription
	classifier := NewComplexityClassifier()
	if classifier == nil {
		t.Fatal("expected non-nil classifier")
	}
	if classifier.model != "claude-haiku-4-5-20251001" {
		t.Errorf("expected haiku model, got %s", classifier.model)
	}
	// Test structured output configuration
	if classifier.useStructuredOutput != false {
		t.Errorf("expected structured output to default to false")
	}
	classifier.SetUseStructuredOutput(true)
	if classifier.useStructuredOutput != true {
		t.Errorf("expected structured output to be set to true")
	}
}

func TestParseClassificationResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Complexity
		wantErr  bool
	}{
		{
			name:     "simple JSON",
			input:    `{"complexity":"MEDIUM","reason":"standard work"}`,
			expected: ComplexityMedium,
		},
		{
			name:     "markdown wrapped",
			input:    "```json\n{\"complexity\":\"COMPLEX\",\"reason\":\"arch change\"}\n```",
			expected: ComplexityComplex,
		},
		{
			name:     "lowercase",
			input:    `{"complexity":"trivial","reason":"typo"}`,
			expected: ComplexityTrivial,
		},
		{
			name:     "epic",
			input:    `{"complexity":"EPIC","reason":"multi-phase"}`,
			expected: ComplexityEpic,
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:    "unknown level",
			input:   `{"complexity":"MEGA","reason":"unknown"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseClassificationResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestParseStructuredClassification(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Complexity
		wantErr  bool
	}{
		{
			name: "valid TRIVIAL classification",
			input: `{
				"result": "Classification complete",
				"session_id": "test-session",
				"structured_output": {"complexity": "TRIVIAL", "reason": "Just a typo fix"}
			}`,
			expected: ComplexityTrivial,
			wantErr:  false,
		},
		{
			name: "valid MEDIUM classification",
			input: `{
				"result": "Classification complete",
				"session_id": "test-session",
				"structured_output": {"complexity": "MEDIUM", "reason": "Standard feature work"}
			}`,
			expected: ComplexityMedium,
			wantErr:  false,
		},
		{
			name: "valid COMPLEX classification",
			input: `{
				"result": "Classification complete",
				"session_id": "test-session",
				"structured_output": {"complexity": "COMPLEX", "reason": "Multiple system changes"}
			}`,
			expected: ComplexityComplex,
			wantErr:  false,
		},
		{
			name: "case insensitive",
			input: `{
				"result": "Classification complete",
				"session_id": "test-session",
				"structured_output": {"complexity": "simple", "reason": "Small change"}
			}`,
			expected: ComplexitySimple,
			wantErr:  false,
		},
		{
			name:    "invalid JSON wrapper",
			input:   `{invalid json}`,
			wantErr: true,
		},
		{
			name: "missing structured_output",
			input: `{
				"result": "Classification complete",
				"session_id": "test-session"
			}`,
			wantErr: true,
		},
		{
			name: "unknown complexity level",
			input: `{
				"result": "Classification complete",
				"session_id": "test-session",
				"structured_output": {"complexity": "UNKNOWN", "reason": "Bad classification"}
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseStructuredClassification([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
