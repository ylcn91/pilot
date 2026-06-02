package memory

import (
	"os"
	"strings"
	"testing"
)

func TestExtractCIErrorPatterns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)

	tests := []struct {
		name           string
		ciLogs         string
		checkNames     []string
		wantMinCount   int // minimum expected patterns
		wantCategory   string
		wantConfidence float64
		wantCIContext  bool
		wantCheckTag   string
	}{
		// --- Compilation category ---
		{
			name:           "compilation: undefined identifier",
			ciLogs:         "undefined: processItem",
			wantMinCount:   1,
			wantCategory:   "compilation",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "compilation: type mismatch",
			ciLogs:         "cannot use myString as int in argument to Foo",
			wantMinCount:   1,
			wantCategory:   "compilation",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "compilation: unused variable",
			ciLogs:         "x declared and not used",
			wantMinCount:   1,
			wantCategory:   "compilation",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "compilation: missing return",
			ciLogs:         "missing return at end of function",
			wantMinCount:   1,
			wantCategory:   "compilation",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		// --- Test failure category ---
		{
			name:           "test: FAIL line",
			ciLogs:         "--- FAIL: TestProcess (0.05s)\n    process_test.go:42: expected nil, got error",
			wantMinCount:   1,
			wantCategory:   "test",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "test: panic test timed out",
			ciLogs:         "panic: test timed out after 30s",
			wantMinCount:   1,
			wantCategory:   "test",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "test: assertion failure",
			ciLogs:         "expected 42 but got 0",
			wantMinCount:   1,
			wantCategory:   "test",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "test: runtime panic",
			ciLogs:         "panic: runtime error: index out of range [5] with length 3",
			wantMinCount:   1,
			wantCategory:   "test",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		// --- Lint category ---
		{
			name:           "lint: golangci-lint",
			ciLogs:         "golangci-lint: error in file main.go",
			wantMinCount:   1,
			wantCategory:   "lint",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "lint: staticcheck",
			ciLogs:         "staticcheck: SA1006 found in handler.go",
			wantMinCount:   1,
			wantCategory:   "lint",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "lint: errcheck",
			ciLogs:         "errcheck: unchecked error in server.go",
			wantMinCount:   1,
			wantCategory:   "lint",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		// --- Build/module category ---
		{
			name:           "build: missing go.sum entry",
			ciLogs:         "missing go.sum entry for module github.com/foo/bar",
			wantMinCount:   1,
			wantCategory:   "build",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "build: import cycle",
			ciLogs:         "import cycle not allowed: pkg/a -> pkg/b -> pkg/a",
			wantMinCount:   1,
			wantCategory:   "build",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		{
			name:           "build: missing dependency",
			ciLogs:         "cannot find package \"github.com/missing/pkg\" in any of:",
			wantMinCount:   1,
			wantCategory:   "build",
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
		// --- Check name tagging ---
		{
			name:           "check name included in context",
			ciLogs:         "undefined: processItem",
			checkNames:     []string{"go-build", "go-vet"},
			wantMinCount:   1,
			wantCategory:   "compilation",
			wantConfidence: 0.5,
			wantCIContext:  true,
			wantCheckTag:   "check:go-build,go-vet",
		},
		// --- Edge cases ---
		{
			name:         "no matching patterns",
			ciLogs:       "Build succeeded. All tests passed.",
			wantMinCount: 0,
		},
		{
			name:         "empty logs",
			ciLogs:       "",
			wantMinCount: 0,
		},
		{
			name:         "whitespace-only logs",
			ciLogs:       "   \n\t  ",
			wantMinCount: 0,
		},
		// --- Multiple categories in one log ---
		{
			name:           "mixed: compilation + test failure",
			ciLogs:         "--- FAIL: TestProcess (0.05s)\npanic: runtime error: nil pointer\nundefined: handler",
			wantMinCount:   3,  // FAIL + runtime panic + undefined
			wantCategory:   "", // mixed, skip category check
			wantConfidence: 0.5,
			wantCIContext:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patterns []*ExtractedPattern
			if len(tt.checkNames) > 0 {
				patterns = extractor.extractCIErrorPatterns(tt.ciLogs, tt.checkNames...)
			} else {
				patterns = extractor.extractCIErrorPatterns(tt.ciLogs)
			}

			if len(patterns) < tt.wantMinCount {
				t.Errorf("got %d patterns, want at least %d", len(patterns), tt.wantMinCount)
				for i, p := range patterns {
					t.Logf("  pattern[%d]: %q category context=%q", i, p.Title, p.Context)
				}
				return
			}

			for _, p := range patterns {
				if p.Confidence != tt.wantConfidence {
					t.Errorf("pattern %q confidence = %f, want %f", p.Title, p.Confidence, tt.wantConfidence)
				}
				if tt.wantCIContext && !strings.HasPrefix(p.Context, "source:ci") {
					t.Errorf("pattern %q context = %q, want source:ci prefix", p.Title, p.Context)
				}
				if tt.wantCategory != "" && !strings.Contains(p.Context, "category:"+tt.wantCategory) {
					t.Errorf("pattern %q context = %q, want category:%s", p.Title, p.Context, tt.wantCategory)
				}
				if tt.wantCheckTag != "" && !strings.Contains(p.Context, tt.wantCheckTag) {
					t.Errorf("pattern %q context = %q, want %s", p.Title, p.Context, tt.wantCheckTag)
				}
			}
		})
	}
}
