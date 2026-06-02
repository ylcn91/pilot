package memory

import (
	"context"
	"os"
	"testing"
)

func TestExtractErrorPatterns_CICompilationErrors(t *testing.T) {
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
		errorOutput      string
		wantAntiPatterns int
		wantTitle        string
	}{
		{
			name:             "undefined identifier",
			errorOutput:      "./main.go:15:2: undefined: myFunc",
			wantAntiPatterns: 1,
			wantTitle:        "Undefined identifier",
		},
		{
			name:             "unused variable",
			errorOutput:      "./handler.go:10:2: x declared and not used",
			wantAntiPatterns: 1,
			wantTitle:        "Unused variable or import",
		},
		{
			name:             "type mismatch",
			errorOutput:      "cannot use val (variable of type string) as int in argument to process",
			wantAntiPatterns: 1,
			wantTitle:        "Type mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &Execution{
				ID:          "test-ci-compile",
				ProjectPath: "/test/project",
				Status:      "completed",
				Output:      "output",
				Error:       tt.errorOutput,
			}

			result, err := extractor.ExtractFromExecution(ctx, exec)
			if err != nil {
				t.Fatalf("ExtractFromExecution failed: %v", err)
			}

			if len(result.AntiPatterns) < tt.wantAntiPatterns {
				t.Errorf("got %d anti-patterns, want at least %d", len(result.AntiPatterns), tt.wantAntiPatterns)
			}

			found := false
			for _, ap := range result.AntiPatterns {
				if ap.Title == tt.wantTitle {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected anti-pattern with title %q not found", tt.wantTitle)
			}
		})
	}
}

func TestExtractErrorPatterns_CITestFailures(t *testing.T) {
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
		errorOutput      string
		wantAntiPatterns int
		wantTitle        string
	}{
		{
			name:             "test FAIL line",
			errorOutput:      "--- FAIL: TestHandler (0.01s)\n    handler_test.go:42: expected 200, got 500",
			wantAntiPatterns: 1,
			wantTitle:        "Test failure",
		},
		{
			name:             "test timeout panic",
			errorOutput:      "panic: test timed out after 30s",
			wantAntiPatterns: 1,
			wantTitle:        "Test timeout",
		},
		{
			name:             "runtime panic in test",
			errorOutput:      "panic: runtime error: index out of range [5] with length 3",
			wantAntiPatterns: 1,
			wantTitle:        "Runtime panic in test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &Execution{
				ID:          "test-ci-test",
				ProjectPath: "/test/project",
				Status:      "completed",
				Output:      "output",
				Error:       tt.errorOutput,
			}

			result, err := extractor.ExtractFromExecution(ctx, exec)
			if err != nil {
				t.Fatalf("ExtractFromExecution failed: %v", err)
			}

			if len(result.AntiPatterns) < tt.wantAntiPatterns {
				t.Errorf("got %d anti-patterns, want at least %d", len(result.AntiPatterns), tt.wantAntiPatterns)
			}

			found := false
			for _, ap := range result.AntiPatterns {
				if ap.Title == tt.wantTitle {
					found = true
					break
				}
			}
			if !found {
				titles := make([]string, len(result.AntiPatterns))
				for i, ap := range result.AntiPatterns {
					titles[i] = ap.Title
				}
				t.Errorf("expected anti-pattern %q not found, got: %v", tt.wantTitle, titles)
			}
		})
	}
}

func TestExtractErrorPatterns_CILintAndBuildErrors(t *testing.T) {
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
		errorOutput      string
		wantAntiPatterns int
		wantTitle        string
	}{
		{
			name:             "golangci-lint error",
			errorOutput:      "golangci-lint: error at handler.go:55 (errcheck)",
			wantAntiPatterns: 1,
			wantTitle:        "Lint violation",
		},
		{
			name:             "staticcheck error",
			errorOutput:      "staticcheck: SA1019 deprecated function used",
			wantAntiPatterns: 1,
			wantTitle:        "Lint violation",
		},
		{
			name:             "missing module",
			errorOutput:      "missing go.sum entry for module providing package github.com/foo/bar",
			wantAntiPatterns: 1,
			wantTitle:        "Missing module",
		},
		{
			name:             "version conflict",
			errorOutput:      `require github.com/foo/bar: version "v1.2.3" invalid`,
			wantAntiPatterns: 1,
			wantTitle:        "Module version conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &Execution{
				ID:          "test-ci-lint",
				ProjectPath: "/test/project",
				Status:      "completed",
				Output:      "output",
				Error:       tt.errorOutput,
			}

			result, err := extractor.ExtractFromExecution(ctx, exec)
			if err != nil {
				t.Fatalf("ExtractFromExecution failed: %v", err)
			}

			if len(result.AntiPatterns) < tt.wantAntiPatterns {
				t.Errorf("got %d anti-patterns, want at least %d", len(result.AntiPatterns), tt.wantAntiPatterns)
			}

			found := false
			for _, ap := range result.AntiPatterns {
				if ap.Title == tt.wantTitle {
					found = true
					break
				}
			}
			if !found {
				titles := make([]string, len(result.AntiPatterns))
				for i, ap := range result.AntiPatterns {
					titles[i] = ap.Title
				}
				t.Errorf("expected anti-pattern %q not found, got: %v", tt.wantTitle, titles)
			}
		})
	}
}
