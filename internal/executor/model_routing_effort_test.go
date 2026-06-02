package executor

import (
	"context"
	"errors"
	"testing"
)

func TestModelRouter_SelectEffort(t *testing.T) {
	tests := []struct {
		name     string
		config   *EffortRoutingConfig
		task     *Task
		expected string
	}{
		{
			name:     "effort routing disabled returns empty",
			config:   &EffortRoutingConfig{Enabled: false, Trivial: "low"},
			task:     &Task{Description: "Fix typo"},
			expected: "",
		},
		{
			name: "trivial task returns low",
			config: &EffortRoutingConfig{
				Enabled: true,
				Trivial: "low",
				Simple:  "medium",
				Medium:  "high",
				Complex: "max",
			},
			task:     &Task{Description: "Fix typo in README"},
			expected: "low",
		},
		{
			name: "complex task returns max",
			config: &EffortRoutingConfig{
				Enabled: true,
				Trivial: "low",
				Simple:  "medium",
				Medium:  "high",
				Complex: "max",
			},
			task:     &Task{Description: "Refactor the authentication system"},
			expected: "max",
		},
		{
			name:     "nil config returns empty",
			config:   nil,
			task:     &Task{Description: "Any task"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewModelRouterWithEffort(nil, nil, tt.config)
			got := router.SelectEffort(tt.task)
			if got != tt.expected {
				t.Errorf("SelectEffort() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestModelRouter_GetEffortForComplexity(t *testing.T) {
	config := &EffortRoutingConfig{
		Enabled: true,
		Trivial: "low",
		Simple:  "medium",
		Medium:  "high",
		Complex: "max",
	}
	router := NewModelRouterWithEffort(nil, nil, config)

	tests := []struct {
		complexity Complexity
		expected   string
	}{
		{ComplexityTrivial, "low"},
		{ComplexitySimple, "medium"},
		{ComplexityMedium, "high"},
		{ComplexityComplex, "max"},
		{Complexity("unknown"), "high"}, // Default to medium complexity
	}

	for _, tt := range tests {
		t.Run(string(tt.complexity), func(t *testing.T) {
			got := router.GetEffortForComplexity(tt.complexity)
			if got != tt.expected {
				t.Errorf("GetEffortForComplexity(%s) = %v, want %v", tt.complexity, got, tt.expected)
			}
		})
	}
}

func TestModelRouter_IsEffortRoutingEnabled(t *testing.T) {
	// Disabled by default
	router := NewModelRouter(nil, nil)
	if router.IsEffortRoutingEnabled() {
		t.Error("Expected effort routing to be disabled by default")
	}

	// Enabled with config
	router = NewModelRouterWithEffort(nil, nil, &EffortRoutingConfig{Enabled: true, Trivial: "low"})
	if !router.IsEffortRoutingEnabled() {
		t.Error("Expected effort routing to be enabled")
	}
}

func TestModelRouter_SelectEffortWithLLMClassifier(t *testing.T) {
	// Test that LLM classifier overrides static mapping
	config := &EffortRoutingConfig{
		Enabled: true,
		Trivial: "low",
		Simple:  "medium",
		Medium:  "high",
		Complex: "max",
	}

	router := NewModelRouterWithEffort(nil, nil, config)

	// Attach a mock classifier that returns "high"
	classifier := newEffortClassifierWithRunner(mockEffortRunner("high", "security sensitive"))
	router.SetEffortClassifier(classifier)

	// This task looks trivial by heuristic, but LLM says "high"
	task := &Task{
		ID:          "GH-100",
		Description: "Fix typo in auth module", // Heuristic: trivial (typo), LLM: high
	}

	got := router.SelectEffort(task)
	if got != "high" {
		t.Errorf("Expected LLM classification 'high' to override static mapping, got %q", got)
	}
}

func TestModelRouter_SelectEffortFallsBackOnLLMFailure(t *testing.T) {
	// Test that static mapping is used when LLM fails
	config := &EffortRoutingConfig{
		Enabled: true,
		Trivial: "low",
		Simple:  "medium",
		Medium:  "high",
		Complex: "max",
	}

	router := NewModelRouterWithEffort(nil, nil, config)

	// Attach a mock classifier that fails
	classifier := newEffortClassifierWithRunner(func(_ context.Context, _ ...string) ([]byte, error) {
		return nil, errors.New("subprocess failed")
	})
	router.SetEffortClassifier(classifier)

	// This task is trivial by heuristic
	task := &Task{
		ID:          "GH-200",
		Description: "Fix typo in README",
	}

	got := router.SelectEffort(task)
	if got != "low" {
		t.Errorf("Expected fallback to static mapping 'low', got %q", got)
	}
}
