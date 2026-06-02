package memory

import (
	"regexp"
	"strings"
)

// codePatternMatcher maps a compiled regex to the pattern metadata it produces.
type codePatternMatcher struct {
	regex   *regexp.Regexp
	pType   PatternType
	title   string
	desc    string
	context string
}

// codePatternMatchers holds all 11 categories of code pattern matchers used by
// extractCodePatterns. len(codePatternMatchers) == 11.
var codePatternMatchers = []codePatternMatcher{
	// 1. Context / cancellation
	{
		regex:   regexp.MustCompile(`(?i)using\s+context\.Context\s+in\s+(\w+)`),
		pType:   PatternTypeCode,
		title:   "Use context.Context for cancellation",
		desc:    "Pass context.Context to functions for proper cancellation and timeout handling",
		context: "Go handlers",
	},
	// 2. Error handling
	{
		regex:   regexp.MustCompile(`(?i)added\s+error\s+handling\s+for\s+(\w+)`),
		pType:   PatternTypeCode,
		title:   "Explicit error handling",
		desc:    "Always handle errors explicitly rather than ignoring them",
		context: "Go functions",
	},
	// 3. Test-driven implementation
	{
		regex:   regexp.MustCompile(`(?i)created?\s+test[s]?\s+for\s+(\w+)`),
		pType:   PatternTypeWorkflow,
		title:   "Test-driven implementation",
		desc:    "Create tests alongside implementation code",
		context: "All code",
	},
	// 4. Structured logging
	{
		regex:   regexp.MustCompile(`(?i)using\s+(zap|slog|logrus)\s+for\s+logging`),
		pType:   PatternTypeCode,
		title:   "Structured logging",
		desc:    "Use structured logging library instead of fmt.Printf",
		context: "Go services",
	},
	// 5. Input validation
	{
		regex:   regexp.MustCompile(`(?i)added?\s+validation\s+for\s+(\w+)`),
		pType:   PatternTypeCode,
		title:   "Input validation",
		desc:    "Validate inputs at system boundaries",
		context: "API handlers",
	},
	// 6. API Design — endpoint patterns, request/response structure, middleware usage
	{
		regex:   regexp.MustCompile(`(?i)(?:http\.Handler|router\.\w+|\bendpoint\b|\bmiddleware\b|\bREST\b|\bGraphQL\b)`),
		pType:   PatternTypeCode,
		title:   "API design pattern",
		desc:    "Organise HTTP handlers, middleware, and routing following REST or GraphQL conventions",
		context: "API design",
	},
	// 7. Concurrency — goroutine patterns, mutex usage, channel patterns, sync primitives
	{
		regex:   regexp.MustCompile(`(?i)(?:go\s+func|sync\.Mutex|sync\.WaitGroup|chan\s+\w+|select\s*\{|sync\.Once)`),
		pType:   PatternTypeCode,
		title:   "Concurrency pattern",
		desc:    "Use goroutines, channels, and sync primitives for safe concurrent access",
		context: "Concurrency",
	},
	// 8. Config wiring — YAML tags, env vars, Viper, mapstructure
	{
		regex:   regexp.MustCompile("(?i)(?:yaml:\"|env:\"|config\\.\\w+|viper\\.\\w+|\\bmapstructure\\b|os\\.Getenv)"),
		pType:   PatternTypeCode,
		title:   "Config wiring pattern",
		desc:    "Load configuration from YAML, environment variables, or Viper with proper struct tags",
		context: "Config wiring",
	},
	// 9. Test patterns — table-driven tests, httptest, mocks, testify assertions
	{
		regex:   regexp.MustCompile(`(?i)(?:t\.Run\s*\(|httptest\.\w+|\bmock\w*\b|\btestify\b|assert\.\w+|require\.\w+)`),
		pType:   PatternTypeWorkflow,
		title:   "Test pattern",
		desc:    "Use table-driven tests, httptest, mocks, and testify assertions for comprehensive coverage",
		context: "Test patterns",
	},
	// 10. Performance — caching, connection pooling, batch ops, indexing
	{
		regex:   regexp.MustCompile(`(?i)(?:\bcache\b|\bpool\b|\bbatch\b|\bindex\b|\bSetMaxOpenConns\b|sync\.Pool)`),
		pType:   PatternTypeCode,
		title:   "Performance pattern",
		desc:    "Apply caching, connection pooling, batching, and indexing for performance-critical paths",
		context: "Performance",
	},
	// 11. Security — auth, token validation, sanitisation, RBAC, HMAC
	{
		regex:   regexp.MustCompile(`(?i)(?:\bauthentication\b|\bauthorization\b|\btoken\s+\w+|\bsanitize\b|\bescape\b|\bpermission\b|\brbac\b|\bhmac\b)`),
		pType:   PatternTypeCode,
		title:   "Security pattern",
		desc:    "Apply authentication, token validation, input sanitisation, RBAC, and HMAC signing",
		context: "Security",
	},
}

// extractCodePatterns extracts code-related patterns
func (e *PatternExtractor) extractCodePatterns(output string) []*ExtractedPattern {
	var patterns []*ExtractedPattern

	for _, matcher := range codePatternMatchers {
		if matches := matcher.regex.FindAllStringSubmatch(output, -1); len(matches) > 0 {
			examples := make([]string, 0, len(matches))
			for _, m := range matches {
				if len(m) > 1 {
					examples = append(examples, m[1])
				}
			}
			patterns = append(patterns, &ExtractedPattern{
				Type:        matcher.pType,
				Title:       matcher.title,
				Description: matcher.desc,
				Examples:    examples,
				Confidence:  0.7, // Base confidence, adjusted by occurrences
				Context:     matcher.context,
			})
		}
	}

	return patterns
}

// extractErrorPatterns extracts patterns from errors (anti-patterns to avoid)
func (e *PatternExtractor) extractErrorPatterns(errorOutput string) []*ExtractedPattern {
	var patterns []*ExtractedPattern

	errorMatchers := []struct {
		regex   *regexp.Regexp
		pType   PatternType
		title   string
		desc    string
		context string
	}{
		{
			regex:   regexp.MustCompile(`(?i)nil\s+pointer\s+dereference`),
			pType:   PatternTypeError,
			title:   "Nil pointer dereference",
			desc:    "Always check for nil before dereferencing pointers",
			context: "Go code",
		},
		{
			regex:   regexp.MustCompile(`(?i)sql:\s+no\s+rows\s+in\s+result\s+set`),
			pType:   PatternTypeError,
			title:   "Handle SQL no rows",
			desc:    "Check for sql.ErrNoRows when querying database",
			context: "Go database code",
		},
		{
			regex:   regexp.MustCompile(`(?i)context\s+deadline\s+exceeded`),
			pType:   PatternTypeError,
			title:   "Context timeout",
			desc:    "Handle context deadline exceeded errors gracefully",
			context: "Async operations",
		},
		{
			regex:   regexp.MustCompile(`(?i)race\s+condition\s+detected`),
			pType:   PatternTypeError,
			title:   "Race condition detected",
			desc:    "Use mutex or channels for concurrent access",
			context: "Concurrent Go code",
		},
		{
			regex:   regexp.MustCompile(`(?i)import\s+cycle\s+not\s+allowed`),
			pType:   PatternTypeStructure,
			title:   "Import cycle",
			desc:    "Restructure packages to avoid import cycles",
			context: "Go package structure",
		},
		// CI compilation errors
		{
			regex:   regexp.MustCompile(`(?i)undefined:\s*\w+`),
			pType:   PatternTypeError,
			title:   "Undefined identifier",
			desc:    "Ensure all identifiers are declared or imported before use",
			context: "Go compilation",
		},
		{
			regex:   regexp.MustCompile(`(?i)\w+\s+declared\s+(?:and\s+)?not\s+used`),
			pType:   PatternTypeError,
			title:   "Unused variable or import",
			desc:    "Remove unused variables and imports to pass compilation",
			context: "Go compilation",
		},
		{
			regex:   regexp.MustCompile(`(?i)cannot\s+use\s+.*\s+as\s+.*\s+in`),
			pType:   PatternTypeError,
			title:   "Type mismatch",
			desc:    "Ensure type compatibility in assignments and function calls",
			context: "Go compilation",
		},
		// CI test failures
		{
			regex:   regexp.MustCompile(`(?m)^---\s+FAIL:\s+\w+`),
			pType:   PatternTypeError,
			title:   "Test failure",
			desc:    "One or more tests failed during CI; investigate and fix failing assertions",
			context: "Go test",
		},
		{
			regex:   regexp.MustCompile(`(?i)panic:\s+test\s+timed\s+out|test\s+.*\s+timed?\s*out`),
			pType:   PatternTypeError,
			title:   "Test timeout",
			desc:    "Test exceeded its deadline; check for blocking operations or infinite loops",
			context: "Go test",
		},
		{
			regex:   regexp.MustCompile(`(?i)panic:\s+runtime\s+error`),
			pType:   PatternTypeError,
			title:   "Runtime panic in test",
			desc:    "A test triggered a runtime panic; add nil checks and bounds validation",
			context: "Go test",
		},
		// CI lint errors
		{
			regex:   regexp.MustCompile(`(?i)(?:golangci-lint|staticcheck|errcheck|govet|gosimple)\s*[:\[]`),
			pType:   PatternTypeWorkflow,
			title:   "Lint violation",
			desc:    "Code does not pass linter checks; fix reported issues before merging",
			context: "Go lint",
		},
		// CI build/module errors
		{
			regex:   regexp.MustCompile(`(?i)missing\s+go\.sum\s+entry|missing\s+module`),
			pType:   PatternTypeError,
			title:   "Missing module",
			desc:    "Run 'go mod tidy' to resolve missing module or go.sum entries",
			context: "Go modules",
		},
		{
			regex:   regexp.MustCompile(`(?i)require\s+.*:\s+version\s+"[^"]*"\s+invalid|go\.mod\s+.*\s+version\s+mismatch`),
			pType:   PatternTypeError,
			title:   "Module version conflict",
			desc:    "Resolve version conflicts in go.mod; ensure compatible dependency versions",
			context: "Go modules",
		},
	}

	for _, matcher := range errorMatchers {
		if matcher.regex.MatchString(errorOutput) {
			patterns = append(patterns, &ExtractedPattern{
				Type:        matcher.pType,
				Title:       matcher.title,
				Description: matcher.desc,
				Examples:    []string{errorOutput[:min(200, len(errorOutput))]},
				Confidence:  0.8, // Higher confidence for errors
				Context:     matcher.context,
			})
		}
	}

	return patterns
}

// extractWorkflowPatterns extracts workflow-related patterns
func (e *PatternExtractor) extractWorkflowPatterns(output string) []*ExtractedPattern {
	var patterns []*ExtractedPattern

	workflowIndicators := []struct {
		indicator string
		pType     PatternType
		title     string
		desc      string
	}{
		{
			indicator: "make test",
			pType:     PatternTypeWorkflow,
			title:     "Run tests via make",
			desc:      "Use make test for consistent test execution",
		},
		{
			indicator: "make lint",
			pType:     PatternTypeWorkflow,
			title:     "Run linter via make",
			desc:      "Use make lint for code quality checks",
		},
		{
			indicator: "git commit",
			pType:     PatternTypeWorkflow,
			title:     "Commit changes",
			desc:      "Commit changes after implementation",
		},
	}

	for _, ind := range workflowIndicators {
		if strings.Contains(strings.ToLower(output), ind.indicator) {
			patterns = append(patterns, &ExtractedPattern{
				Type:        ind.pType,
				Title:       ind.title,
				Description: ind.desc,
				Confidence:  0.6,
			})
		}
	}

	return patterns
}
