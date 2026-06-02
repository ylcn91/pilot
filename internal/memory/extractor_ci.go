package memory

import (
	"fmt"
	"regexp"
	"strings"
)

// ciCategoryMatcher defines a CI error pattern with its category classification.
type ciCategoryMatcher struct {
	regex    *regexp.Regexp
	pType    PatternType
	title    string
	desc     string
	category string // compilation, test, lint, build
}

// ciMatchers are pre-compiled matchers organized by CI error category.
var ciMatchers = []ciCategoryMatcher{
	// --- Compilation errors ---
	{
		regex:    regexp.MustCompile(`(?i)undefined:\s*\w+`),
		pType:    PatternTypeError,
		title:    "Undefined identifier",
		desc:     "Ensure all identifiers are declared or imported before use",
		category: "compilation",
	},
	{
		regex:    regexp.MustCompile(`(?i)cannot\s+use\s+.*\s+as\s+.*\s+in`),
		pType:    PatternTypeError,
		title:    "Type mismatch",
		desc:     "Ensure type compatibility in assignments and function calls",
		category: "compilation",
	},
	{
		regex:    regexp.MustCompile(`(?i)\w+\s+declared\s+(?:and\s+)?not\s+used`),
		pType:    PatternTypeError,
		title:    "Unused variable or import",
		desc:     "Remove unused variables and imports to pass compilation",
		category: "compilation",
	},
	{
		regex:    regexp.MustCompile(`(?i)missing\s+return\s+at\s+end\s+of\s+function`),
		pType:    PatternTypeError,
		title:    "Missing return",
		desc:     "All code paths in a function must end with a return statement",
		category: "compilation",
	},
	// --- Test failures ---
	{
		regex:    regexp.MustCompile(`(?m)^---\s+FAIL:\s+\w+`),
		pType:    PatternTypeError,
		title:    "Test failure",
		desc:     "One or more tests failed during CI; investigate and fix failing assertions",
		category: "test",
	},
	{
		regex:    regexp.MustCompile(`(?i)panic:\s+test\s+timed\s+out|test\s+.*\s+timed?\s*out`),
		pType:    PatternTypeError,
		title:    "Test timeout",
		desc:     "Test exceeded its deadline; check for blocking operations or infinite loops",
		category: "test",
	},
	{
		regex:    regexp.MustCompile(`(?i)(?:expected\s+.*\s+(?:but\s+)?got|assert(?:ion)?\s+fail|Expected\s+.*\s+to\s+(?:equal|be|match))`),
		pType:    PatternTypeError,
		title:    "Assertion failure",
		desc:     "Test assertion did not match expected value; verify logic and test data",
		category: "test",
	},
	{
		regex:    regexp.MustCompile(`(?i)panic:\s+runtime\s+error`),
		pType:    PatternTypeError,
		title:    "Runtime panic in test",
		desc:     "A test triggered a runtime panic; add nil checks and bounds validation",
		category: "test",
	},
	// --- Lint errors ---
	{
		regex:    regexp.MustCompile(`(?i)golangci-lint\s*[:\[]`),
		pType:    PatternTypeWorkflow,
		title:    "golangci-lint violation",
		desc:     "Code does not pass golangci-lint checks; fix reported issues before merging",
		category: "lint",
	},
	{
		regex:    regexp.MustCompile(`(?i)staticcheck\s*[:\[]`),
		pType:    PatternTypeWorkflow,
		title:    "staticcheck finding",
		desc:     "staticcheck found issues; address reported diagnostics",
		category: "lint",
	},
	{
		regex:    regexp.MustCompile(`(?i)errcheck\s*[:\[]`),
		pType:    PatternTypeWorkflow,
		title:    "errcheck violation",
		desc:     "Unchecked error return values detected; handle all returned errors",
		category: "lint",
	},
	// --- Build/module errors ---
	{
		regex:    regexp.MustCompile(`(?i)missing\s+go\.sum\s+entry|missing\s+module`),
		pType:    PatternTypeError,
		title:    "Missing module",
		desc:     "Run 'go mod tidy' to resolve missing module or go.sum entries",
		category: "build",
	},
	{
		regex:    regexp.MustCompile(`(?i)require\s+.*:\s+version\s+"[^"]*"\s+invalid|go\.mod\s+.*\s+version\s+mismatch`),
		pType:    PatternTypeError,
		title:    "Module version conflict",
		desc:     "Resolve version conflicts in go.mod; ensure compatible dependency versions",
		category: "build",
	},
	{
		regex:    regexp.MustCompile(`(?i)import\s+cycle\s+not\s+allowed`),
		pType:    PatternTypeStructure,
		title:    "Import cycle",
		desc:     "Restructure packages to avoid import cycles",
		category: "build",
	},
	{
		regex:    regexp.MustCompile(`(?i)cannot\s+find\s+package|could\s+not\s+import`),
		pType:    PatternTypeError,
		title:    "Missing dependency",
		desc:     "Required package not found; check import paths and run 'go mod tidy'",
		category: "build",
	},
}

// extractCIErrorPatterns extracts error patterns from CI logs using CI-specific
// category matchers. Each pattern is tagged with source:ci, its category
// (compilation/test/lint/build), and optional check names. Confidence starts at
// 0.5 for CI-sourced patterns (boosted on recurrence via SaveExtractedPatterns).
// Returns empty result for blank ciLogs.
func (e *PatternExtractor) extractCIErrorPatterns(ciLogs string, checkNames ...string) []*ExtractedPattern {
	if strings.TrimSpace(ciLogs) == "" {
		return nil
	}

	var patterns []*ExtractedPattern

	for _, m := range ciMatchers {
		if !m.regex.MatchString(ciLogs) {
			continue
		}

		ctx := fmt.Sprintf("source:ci category:%s", m.category)
		if len(checkNames) > 0 {
			ctx += " check:" + strings.Join(checkNames, ",")
		}

		patterns = append(patterns, &ExtractedPattern{
			Type:        m.pType,
			Title:       m.title,
			Description: m.desc,
			Examples:    []string{ciLogs[:min(200, len(ciLogs))]},
			Confidence:  0.5,
			Context:     ctx,
		})
	}

	return patterns
}
