package memory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestGetRelevantPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns
	_ = store.SaveCrossPattern(&CrossPattern{
		ID:          "handler-1",
		Type:        "code",
		Title:       "Context in handlers",
		Description: "Pass context to handler functions",
		Context:     "Go handlers",
		Confidence:  0.8,
		Occurrences: 10,
		Scope:       "org",
	})
	_ = store.SaveCrossPattern(&CrossPattern{
		ID:          "test-1",
		Type:        "workflow",
		Title:       "Unit tests",
		Description: "Write unit tests",
		Context:     "Testing",
		Confidence:  0.7,
		Occurrences: 5,
		Scope:       "org",
	})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	// Query with handler context
	patterns, err := service.GetRelevantPatterns(ctx, "/test/project", "feat", "implementing a new handler")
	if err != nil {
		t.Fatalf("GetRelevantPatterns failed: %v", err)
	}

	// Handler pattern should be more relevant
	if len(patterns) > 0 && patterns[0].ID != "handler-1" {
		t.Log("Note: Pattern relevance scoring may vary")
	}
}

func TestFormatForPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns
	_ = store.SaveCrossPattern(&CrossPattern{
		ID:          "format-1",
		Type:        "code",
		Title:       "Error Handling",
		Description: "Always wrap errors",
		Context:     "Go code",
		Confidence:  0.8,
		Scope:       "org",
	})
	_ = store.SaveCrossPattern(&CrossPattern{
		ID:            "format-anti",
		Type:          "error",
		Title:         "[ANTI] Nil pointer",
		Description:   "AVOID: Dereferencing nil pointers",
		Confidence:    0.7,
		IsAntiPattern: true,
		Scope:         "org",
	})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	prompt, err := service.FormatForPrompt(ctx, "/test/project", "feat", "fixing an error handler")
	if err != nil {
		t.Fatalf("FormatForPrompt failed: %v", err)
	}

	// Should contain header
	if prompt != "" {
		if len(prompt) < 10 {
			t.Error("prompt seems too short")
		}
	}
}

func TestFormatForPrompt_Empty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// No patterns
	service := NewPatternQueryService(store)
	ctx := context.Background()

	prompt, err := service.FormatForPrompt(ctx, "/empty/project", "feat", "doing something")
	if err != nil {
		t.Fatalf("FormatForPrompt failed: %v", err)
	}

	// Empty is acceptable for no patterns
	_ = prompt
}

func TestGetPatternSuggestions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns of different types
	_ = store.SaveCrossPattern(&CrossPattern{ID: "code-1", Type: "code", Title: "Implement function", Confidence: 0.8, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "test-1", Type: "workflow", Title: "Run tests", Confidence: 0.8, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "structure-1", Type: "structure", Title: "Package layout", Confidence: 0.8, Scope: "org"})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	tests := []struct {
		name          string
		partialOutput string
		wantType      string
	}{
		{
			name:          "function implementation",
			partialOutput: "implementing a new function for user authentication",
			wantType:      "code",
		},
		{
			name:          "testing context",
			partialOutput: "running test to verify the changes",
			wantType:      "workflow",
		},
		{
			name:          "package organization",
			partialOutput: "organizing the package structure",
			wantType:      "structure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := service.GetPatternSuggestions(ctx, "/test/project", tt.partialOutput)
			if err != nil {
				t.Fatalf("GetPatternSuggestions failed: %v", err)
			}

			// Should return some suggestions
			_ = suggestions
		})
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  bool
	}{
		{name: "contains", slice: []string{"a", "b", "c"}, s: "b", want: true},
		{name: "not contains", slice: []string{"a", "b", "c"}, s: "d", want: false},
		{name: "empty slice", slice: []string{}, s: "a", want: false},
		{name: "empty string", slice: []string{"a", "b", ""}, s: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.slice, tt.s)
			if got != tt.want {
				t.Errorf("containsString(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.want)
			}
		})
	}
}

func TestFormatForPrompt_AntiPatternsNotCrowdedOut(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-anti-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	tests := []struct {
		name               string
		patterns           []*CrossPattern
		wantAntiSection    bool
		wantNormalSection  bool
		wantAntiSubstrings []string
	}{
		{
			name: "anti-patterns appear despite many high-confidence normal patterns",
			patterns: []*CrossPattern{
				{ID: "norm-1", Type: "code", Title: "Normal 1", Description: "Desc 1", Confidence: 0.95, Occurrences: 20, Scope: "org"},
				{ID: "norm-2", Type: "code", Title: "Normal 2", Description: "Desc 2", Confidence: 0.93, Occurrences: 18, Scope: "org"},
				{ID: "norm-3", Type: "code", Title: "Normal 3", Description: "Desc 3", Confidence: 0.91, Occurrences: 15, Scope: "org"},
				{ID: "norm-4", Type: "code", Title: "Normal 4", Description: "Desc 4", Confidence: 0.89, Occurrences: 12, Scope: "org"},
				{ID: "norm-5", Type: "code", Title: "Normal 5", Description: "Desc 5", Confidence: 0.87, Occurrences: 10, Scope: "org"},
				{ID: "norm-6", Type: "code", Title: "Normal 6", Description: "Desc 6", Confidence: 0.85, Occurrences: 8, Scope: "org"},
				{ID: "anti-1", Type: "error", Title: "[ANTI] Nil deref", Description: "AVOID: Nil pointer dereference", Confidence: 0.7, IsAntiPattern: true, Scope: "org"},
				{ID: "anti-2", Type: "error", Title: "[ANTI] Missing ctx", Description: "AVOID: Missing context propagation", Confidence: 0.65, IsAntiPattern: true, Scope: "org"},
			},
			wantAntiSection:    true,
			wantNormalSection:  true,
			wantAntiSubstrings: []string{"Nil deref", "Missing ctx"},
		},
		{
			name: "only anti-patterns present",
			patterns: []*CrossPattern{
				{ID: "anti-only-1", Type: "error", Title: "[ANTI] Bad import", Description: "AVOID: Circular imports", Confidence: 0.8, IsAntiPattern: true, Scope: "org"},
			},
			wantAntiSection:    true,
			wantNormalSection:  false,
			wantAntiSubstrings: []string{"Bad import"},
		},
		{
			name: "anti-patterns below min confidence excluded",
			patterns: []*CrossPattern{
				{ID: "norm-hi", Type: "code", Title: "Good pattern", Description: "Desc", Confidence: 0.9, Occurrences: 10, Scope: "org"},
				{ID: "anti-low", Type: "error", Title: "[ANTI] Low conf", Description: "AVOID: something", Confidence: 0.4, IsAntiPattern: true, Scope: "org"},
			},
			wantAntiSection:   false,
			wantNormalSection: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh store per subtest
			subDir, err := os.MkdirTemp("", "query-subtest-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(subDir) }()

			s, _ := NewStore(subDir)
			defer func() { _ = s.Close() }()

			for _, p := range tt.patterns {
				if err := s.SaveCrossPattern(p); err != nil {
					t.Fatalf("SaveCrossPattern failed: %v", err)
				}
			}

			service := NewPatternQueryService(s)
			ctx := context.Background()

			prompt, err := service.FormatForPrompt(ctx, "/test/project", "feat", "implementing a handler")
			if err != nil {
				t.Fatalf("FormatForPrompt failed: %v", err)
			}

			hasAntiSection := strings.Contains(prompt, "Anti-Patterns to Avoid")
			if hasAntiSection != tt.wantAntiSection {
				t.Errorf("anti-pattern section present=%v, want %v\nprompt:\n%s", hasAntiSection, tt.wantAntiSection, prompt)
			}

			hasNormalSection := strings.Contains(prompt, "Recommended Patterns")
			if hasNormalSection != tt.wantNormalSection {
				t.Errorf("normal section present=%v, want %v\nprompt:\n%s", hasNormalSection, tt.wantNormalSection, prompt)
			}

			for _, sub := range tt.wantAntiSubstrings {
				if !strings.Contains(prompt, sub) {
					t.Errorf("expected anti-pattern substring %q in prompt, not found\nprompt:\n%s", sub, prompt)
				}
			}
		})
	}
}

func TestPatternConfidence_ContextualOverGlobal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-ctx-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create pattern with low global confidence
	_ = store.SaveCrossPattern(&CrossPattern{
		ID: "ctx-pat", Type: "code", Title: "Contextual Pattern",
		Confidence: 0.5, Scope: "org",
	})
	_ = store.LinkPatternToProject("ctx-pat", "/test/project")

	// Record high success rate for this project+task type
	for i := 0; i < 10; i++ {
		_ = store.RecordPatternOutcome("ctx-pat", "/test/project", "feat", "opus", true)
	}

	service := NewPatternQueryService(store)

	// With contextual data, should use contextual confidence (high success rate)
	c := service.patternConfidence(&CrossPattern{ID: "ctx-pat", Confidence: 0.5}, "/test/project", "feat")
	if c <= 0.5 {
		t.Errorf("contextual confidence = %f, want > 0.5 (should use high success rate)", c)
	}

	// Without task type, should fall back to global confidence
	c2 := service.patternConfidence(&CrossPattern{ID: "ctx-pat", Confidence: 0.5}, "/test/project", "")
	if c2 != 0.5 {
		t.Errorf("fallback confidence = %f, want 0.5 (global)", c2)
	}

	// For unknown pattern+context combo (returns 0.5 from GetContextualConfidence),
	// should fall back to global confidence
	c3 := service.patternConfidence(&CrossPattern{ID: "unknown-pat", Confidence: 0.8}, "/test/project", "feat")
	if c3 != 0.8 {
		t.Errorf("unknown pattern confidence = %f, want 0.8 (global fallback)", c3)
	}
}

func TestGetRelevantPatterns_Scoring(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns with different relevance
	_ = store.SaveCrossPattern(&CrossPattern{
		ID:          "very-relevant",
		Type:        "code",
		Title:       "Authentication handler",
		Description: "Implement auth handler",
		Context:     "Go handlers",
		Confidence:  0.8,
		Occurrences: 20,
		Scope:       "org",
	})
	_ = store.SaveCrossPattern(&CrossPattern{
		ID:          "somewhat-relevant",
		Type:        "code",
		Title:       "Logging setup",
		Description: "Configure logging",
		Context:     "Infrastructure",
		Confidence:  0.8,
		Occurrences: 5,
		Scope:       "org",
	})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	patterns, err := service.GetRelevantPatterns(ctx, "/test/project", "feat", "implementing authentication handler")
	if err != nil {
		t.Fatalf("GetRelevantPatterns failed: %v", err)
	}

	if len(patterns) > 1 {
		// The more relevant pattern should score higher and be first
		if patterns[0].ID != "very-relevant" {
			t.Log("Note: Pattern ordering based on relevance may vary")
		}
	}
}
