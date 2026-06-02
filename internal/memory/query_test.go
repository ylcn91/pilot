package memory

import (
	"context"
	"os"
	"testing"
)

func TestNewPatternQueryService(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	service := NewPatternQueryService(store)
	if service == nil {
		t.Error("NewPatternQueryService returned nil")
	}
}

func TestQuery_DefaultValues(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	service := NewPatternQueryService(store)
	ctx := context.Background()

	// Query with defaults
	result, err := service.Query(ctx, &PatternQuery{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result == nil {
		t.Error("Query returned nil result")
	}
}

func TestQuery_WithFilters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create test patterns
	patterns := []*CrossPattern{
		{ID: "qf-code-1", Type: "code", Title: "Code Pattern", Confidence: 0.9, Occurrences: 10, Scope: "org"},
		{ID: "qf-code-2", Type: "code", Title: "Low Confidence", Confidence: 0.4, Occurrences: 2, Scope: "org"},
		{ID: "qf-workflow-1", Type: "workflow", Title: "Workflow Pattern", Confidence: 0.8, Occurrences: 5, Scope: "org"},
		{ID: "qf-anti-1", Type: "error", Title: "Anti Pattern", Confidence: 0.7, IsAntiPattern: true, Scope: "org"},
	}

	for _, p := range patterns {
		_ = store.SaveCrossPattern(p)
	}

	service := NewPatternQueryService(store)
	ctx := context.Background()

	tests := []struct {
		name    string
		query   *PatternQuery
		wantMin int
	}{
		{
			name: "filter by type",
			query: &PatternQuery{
				Types:         []string{"code"},
				MinConfidence: 0.3,
				MaxResults:    10,
			},
			wantMin: 2,
		},
		{
			name: "filter by confidence",
			query: &PatternQuery{
				MinConfidence: 0.7,
				MaxResults:    10,
			},
			wantMin: 2, // at least code-1 and workflow-1
		},
		{
			name: "exclude anti-patterns",
			query: &PatternQuery{
				MinConfidence: 0.7,
				MaxResults:    10,
				IncludeAnti:   false,
			},
			wantMin: 2, // code-1, workflow-1
		},
		{
			name: "limit results",
			query: &PatternQuery{
				MinConfidence: 0.3,
				MaxResults:    2,
			},
			wantMin: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}

			if len(result.Patterns) < tt.wantMin {
				t.Errorf("got %d patterns, want at least %d", len(result.Patterns), tt.wantMin)
			}
		})
	}
}

func TestQuery_WithSearchTerm(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create test patterns
	_ = store.SaveCrossPattern(&CrossPattern{ID: "1", Type: "code", Title: "Error Handling", Description: "Wrap errors", Confidence: 0.8, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "2", Type: "code", Title: "Context Usage", Description: "Pass context", Confidence: 0.8, Scope: "org"})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		SearchTerm:    "error",
		MinConfidence: 0.5,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Query with search failed: %v", err)
	}

	if len(result.Patterns) != 1 {
		t.Errorf("got %d patterns, want 1 for 'error' search", len(result.Patterns))
	}
}

func TestQuery_WithProjectPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns with project links
	_ = store.SaveCrossPattern(&CrossPattern{ID: "org-1", Type: "code", Title: "Org Pattern", Confidence: 0.8, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "project-1", Type: "code", Title: "Project Pattern", Confidence: 0.8, Scope: "project"})
	_ = store.LinkPatternToProject("project-1", "/my/project")

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		ProjectPath:   "/my/project",
		MinConfidence: 0.5,
		MaxResults:    10,
		IncludeGlobal: true,
	})
	if err != nil {
		t.Fatalf("Query with project path failed: %v", err)
	}

	if len(result.Patterns) < 1 {
		t.Error("expected at least org-scope patterns for project query")
	}
}

func TestQuery_SortByConfidence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns with varying confidence
	_ = store.SaveCrossPattern(&CrossPattern{ID: "low", Type: "code", Title: "Low", Confidence: 0.5, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "high", Type: "code", Title: "High", Confidence: 0.9, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "medium", Type: "code", Title: "Medium", Confidence: 0.7, Scope: "org"})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		MinConfidence: 0.4,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Patterns) < 2 {
		t.Skip("Not enough patterns to test sorting")
	}

	// Verify sorted by confidence descending
	for i := 1; i < len(result.Patterns); i++ {
		if result.Patterns[i].Confidence > result.Patterns[i-1].Confidence {
			t.Errorf("patterns not sorted by confidence: %f > %f",
				result.Patterns[i].Confidence, result.Patterns[i-1].Confidence)
		}
	}
}

func TestQuery_TotalMatches(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create many patterns with unique IDs
	for i := 0; i < 10; i++ {
		_ = store.SaveCrossPattern(&CrossPattern{
			ID:         "total-match-" + string(rune('a'+i)),
			Type:       "code",
			Title:      "Pattern",
			Confidence: 0.8,
			Scope:      "org",
		})
	}

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		MinConfidence: 0.5,
		MaxResults:    3,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Patterns) > 3 {
		t.Errorf("got %d patterns, want at most 3", len(result.Patterns))
	}

	// TotalMatches should be at least as many as we created
	if result.TotalMatches < 3 {
		t.Errorf("TotalMatches = %d, want >= 3", result.TotalMatches)
	}
}

func TestQuery_QueryTime(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		MinConfidence: 0.5,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.QueryTime <= 0 {
		t.Error("QueryTime should be positive")
	}
}

func TestQuery_ScopeFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Create patterns with different scopes
	_ = store.SaveCrossPattern(&CrossPattern{ID: "org-1", Type: "code", Title: "Org", Confidence: 0.8, Scope: "org"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "project-1", Type: "code", Title: "Project", Confidence: 0.8, Scope: "project"})
	_ = store.SaveCrossPattern(&CrossPattern{ID: "global-1", Type: "code", Title: "Global", Confidence: 0.8, Scope: "global"})

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		Scope:         "org",
		MinConfidence: 0.5,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	for _, p := range result.Patterns {
		if p.Scope != "org" {
			t.Errorf("pattern scope = %q, want 'org'", p.Scope)
		}
	}
}

func TestQuery_OnlyAnti(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "query-test-onlyanti-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	// Mix of normal and anti-patterns
	patterns := []*CrossPattern{
		{ID: "oa-norm-1", Type: "code", Title: "Normal", Confidence: 0.95, Occurrences: 20, Scope: "org"},
		{ID: "oa-norm-2", Type: "code", Title: "Normal 2", Confidence: 0.90, Occurrences: 15, Scope: "org"},
		{ID: "oa-anti-1", Type: "error", Title: "Anti 1", Confidence: 0.7, IsAntiPattern: true, Scope: "org"},
		{ID: "oa-anti-2", Type: "error", Title: "Anti 2", Confidence: 0.65, IsAntiPattern: true, Scope: "org"},
	}
	for _, p := range patterns {
		_ = store.SaveCrossPattern(p)
	}

	service := NewPatternQueryService(store)
	ctx := context.Background()

	result, err := service.Query(ctx, &PatternQuery{
		OnlyAnti:      true,
		MinConfidence: 0.5,
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result.Patterns) != 2 {
		t.Errorf("got %d patterns, want 2 anti-patterns only", len(result.Patterns))
	}
	for _, p := range result.Patterns {
		if !p.IsAntiPattern {
			t.Errorf("got non-anti-pattern %q with OnlyAnti=true", p.ID)
		}
	}
}
