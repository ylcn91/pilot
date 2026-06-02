package memory

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExtractFromSelfReview(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	tests := []struct {
		name             string
		selfReviewOutput string
		wantAntiPatterns int
		wantPatternTypes []PatternType // expected types in anti-patterns (order-independent)
		wantTier         string        // expected ExtractionResult.Tier (empty unless STANDARD_VIOLATION)
	}{
		{
			name: "multiple finding types",
			selfReviewOutput: `REVIEW_FIXED: corrected nil check in handler.go
Found missing error handling in database query at store.go:42
PARITY_GAP: interface method added but not implemented in mock
test coverage gap for new validateInput function
SUSPICIOUS_VALUE detected in pricing constant`,
			wantAntiPatterns: 5,
			wantPatternTypes: []PatternType{
				PatternTypeCode,      // REVIEW_FIXED
				PatternTypeError,     // missing error handling
				PatternTypeStructure, // PARITY_GAP
				PatternTypeWorkflow,  // test coverage gap
				PatternTypeCode,      // SUSPICIOUS_VALUE
			},
		},
		{
			name:             "empty output",
			selfReviewOutput: "",
			wantAntiPatterns: 0,
			wantPatternTypes: nil,
		},
		{
			name:             "whitespace only output",
			selfReviewOutput: "   \n\t  \n  ",
			wantAntiPatterns: 0,
			wantPatternTypes: nil,
		},
		{
			name:             "unparseable output with no markers",
			selfReviewOutput: "All checks passed. Code looks clean. No issues found.",
			wantAntiPatterns: 0,
			wantPatternTypes: nil,
		},
		{
			name: "mixed findings with clean output",
			selfReviewOutput: `Self-review complete.
Files checked: 5
REVIEW_FIXED: corrected nil check in handler
Everything else looks good.
INCOMPLETE: TODO items remain in auth module`,
			wantAntiPatterns: 2,
			wantPatternTypes: []PatternType{
				PatternTypeCode,     // REVIEW_FIXED
				PatternTypeWorkflow, // INCOMPLETE
			},
		},
		{
			name:             "build failure only",
			selfReviewOutput: "build verification failure: missing return statement in Calculate()",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeError},
		},
		{
			name:             "dead code detection",
			selfReviewOutput: "Found unused import: fmt in utils.go\nFound dead code in legacy handler",
			wantAntiPatterns: 1, // single regex matches both
			wantPatternTypes: []PatternType{PatternTypeCode},
		},
		{
			name:             "struct field unwired",
			selfReviewOutput: "config field not wired: MaxRetries defined in Config but never read",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeStructure},
		},
		{
			name:             "lint violations",
			selfReviewOutput: "lint violation: exported function missing doc comment\nlinter error on line 55",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeWorkflow},
		},
		{
			name: "cross-file parity",
			selfReviewOutput: `cross-file parity issue: AddUser added to service.go but not to service_test.go
parity mismatch between interface and implementation`,
			wantAntiPatterns: 1, // single regex matches both variants
			wantPatternTypes: []PatternType{PatternTypeStructure},
		},
		{
			name:             "scope creep marker",
			selfReviewOutput: "SCOPE_CREEP: renameHelper — touched unrelated util outside task scope",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeStructure},
		},
		{
			name:             "standard violation captures blocker tier",
			selfReviewOutput: "STANDARD_VIOLATION: blocker no-secrets — config.go:12",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeWorkflow},
			wantTier:         "blocker",
		},
		{
			name:             "standard violation captures must tier",
			selfReviewOutput: "STANDARD_VIOLATION: must error-check — handler.go:88",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeWorkflow},
			wantTier:         "must",
		},
		{
			name:             "standard violation captures nice tier",
			selfReviewOutput: "STANDARD_VIOLATION: nice naming — store.go:5",
			wantAntiPatterns: 1,
			wantPatternTypes: []PatternType{PatternTypeWorkflow},
			wantTier:         "nice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractor.ExtractFromSelfReview(ctx, tt.selfReviewOutput, "/test/project")
			if err != nil {
				t.Fatalf("ExtractFromSelfReview() error = %v", err)
			}

			if result == nil {
				t.Fatal("ExtractFromSelfReview() returned nil result")
			}

			if len(result.AntiPatterns) != tt.wantAntiPatterns {
				types := make([]PatternType, len(result.AntiPatterns))
				for i, ap := range result.AntiPatterns {
					types[i] = ap.Type
				}
				t.Errorf("got %d anti-patterns %v, want %d", len(result.AntiPatterns), types, tt.wantAntiPatterns)
				return
			}

			// Verify pattern types match (order-independent)
			if tt.wantPatternTypes != nil {
				gotTypes := make(map[PatternType]int)
				for _, ap := range result.AntiPatterns {
					gotTypes[ap.Type]++
				}

				wantTypes := make(map[PatternType]int)
				for _, pt := range tt.wantPatternTypes {
					wantTypes[pt]++
				}

				for pt, count := range wantTypes {
					if gotTypes[pt] != count {
						t.Errorf("pattern type %s: got %d, want %d", pt, gotTypes[pt], count)
					}
				}
			}

			// Verify all anti-patterns have confidence 0.5
			for _, ap := range result.AntiPatterns {
				if ap.Confidence != 0.5 {
					t.Errorf("anti-pattern %q has confidence %f, want 0.5", ap.Title, ap.Confidence)
				}
				if ap.Context != "Self-review" {
					t.Errorf("anti-pattern %q has context %q, want 'Self-review'", ap.Title, ap.Context)
				}
			}

			// Verify project path is set
			if result.ProjectPath != "/test/project" {
				t.Errorf("ProjectPath = %q, want %q", result.ProjectPath, "/test/project")
			}

			// Verify no positive patterns (self-review findings are all anti-patterns)
			if len(result.Patterns) != 0 {
				t.Errorf("got %d positive patterns, want 0", len(result.Patterns))
			}

			// Verify severity tier parsed from STANDARD_VIOLATION markers
			if result.Tier != tt.wantTier {
				t.Errorf("Tier = %q, want %q", result.Tier, tt.wantTier)
			}
		})
	}
}

// TestCodePatternMatcherCount verifies exactly 11 categories are registered.
func TestCodePatternMatcherCount(t *testing.T) {
	if got := len(codePatternMatchers); got != 11 {
		t.Errorf("len(codePatternMatchers) = %d, want 11", got)
	}
}

// TestNewCodePatternCategories verifies the 6 new matcher categories (API Design,
// Concurrency, Config Wiring, Test Patterns, Performance, Security) each fire on
// matching input and stay silent on unrelated input.
func TestNewCodePatternCategories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-new-cats-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)

	tests := []struct {
		name      string
		output    string
		wantTitle string // substring of expected pattern title
		wantMatch bool
	}{
		// --- API Design ---
		{
			name:      "api design: http.Handler match",
			output:    "implemented http.Handler interface for the webhook route",
			wantTitle: "API design",
			wantMatch: true,
		},
		{
			name:      "api design: REST endpoint match",
			output:    "added REST endpoint with middleware for rate limiting",
			wantTitle: "API design",
			wantMatch: true,
		},
		{
			name:      "api design: no match",
			output:    "wrote a helper function to format timestamps",
			wantTitle: "API design",
			wantMatch: false,
		},
		// --- Concurrency ---
		{
			name:      "concurrency: sync.Mutex match",
			output:    "protected shared counter using sync.Mutex to avoid data races",
			wantTitle: "Concurrency",
			wantMatch: true,
		},
		{
			name:      "concurrency: go func match",
			output:    "launched background worker via go func with sync.WaitGroup",
			wantTitle: "Concurrency",
			wantMatch: true,
		},
		{
			name:      "concurrency: no match",
			output:    "added a simple sequential loop to process items",
			wantTitle: "Concurrency",
			wantMatch: false,
		},
		// --- Config Wiring ---
		{
			name:      "config wiring: yaml tag match",
			output:    `added yaml:"timeout" struct tag and os.Getenv fallback`,
			wantTitle: "Config wiring",
			wantMatch: true,
		},
		{
			name:      "config wiring: viper match",
			output:    "loading settings via viper.GetString and mapstructure decode",
			wantTitle: "Config wiring",
			wantMatch: true,
		},
		{
			name:      "config wiring: no match",
			output:    "refactored the retry loop to use exponential backoff",
			wantTitle: "Config wiring",
			wantMatch: false,
		},
		// --- Test Patterns ---
		{
			name:      "test patterns: t.Run match",
			output:    "added table-driven tests using t.Run( for each scenario",
			wantTitle: "Test pattern",
			wantMatch: true,
		},
		{
			name:      "test patterns: httptest match",
			output:    "wrote handler test with httptest.NewRecorder and assert.Equal",
			wantTitle: "Test pattern",
			wantMatch: true,
		},
		{
			name:      "test patterns: no match",
			output:    "updated the deployment manifest",
			wantTitle: "Test pattern",
			wantMatch: false,
		},
		// --- Performance ---
		{
			name:      "performance: SetMaxOpenConns match",
			output:    "tuned database pool with SetMaxOpenConns to reduce contention",
			wantTitle: "Performance",
			wantMatch: true,
		},
		{
			name:      "performance: cache match",
			output:    "added in-memory cache layer to avoid redundant database queries",
			wantTitle: "Performance",
			wantMatch: true,
		},
		{
			name:      "performance: no match",
			output:    "formatted the codebase with gofmt",
			wantTitle: "Performance",
			wantMatch: false,
		},
		// --- Security ---
		{
			name:      "security: authentication match",
			output:    "added authentication middleware with token validation",
			wantTitle: "Security",
			wantMatch: true,
		},
		{
			name:      "security: hmac match",
			output:    "signing webhook payloads with hmac and checking permission on every call",
			wantTitle: "Security",
			wantMatch: true,
		},
		{
			name:      "security: no match",
			output:    "added structured logging to the service startup path",
			wantTitle: "Security",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &Execution{
				ID:          "test-exec",
				ProjectPath: "/test/project",
				Status:      "completed",
				Output:      tt.output,
			}

			result, err := extractor.ExtractFromExecution(context.Background(), exec)
			if err != nil {
				t.Fatalf("ExtractFromExecution failed: %v", err)
			}

			found := false
			for _, p := range result.Patterns {
				if strings.Contains(p.Title, tt.wantTitle) {
					found = true
					break
				}
			}

			if found != tt.wantMatch {
				if tt.wantMatch {
					t.Errorf("expected pattern with title containing %q, got none (patterns: %v)",
						tt.wantTitle, patternTitles(result.Patterns))
				} else {
					t.Errorf("expected no pattern with title containing %q, but found one",
						tt.wantTitle)
				}
			}
		})
	}
}
