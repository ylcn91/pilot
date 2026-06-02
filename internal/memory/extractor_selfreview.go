package memory

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// selfReviewFinding defines a mapping from self-review markers to pattern types
type selfReviewFinding struct {
	regex   *regexp.Regexp
	pType   PatternType
	title   string
	desc    string
	context string
}

// selfReviewFindings are the matchers for self-review output markers
var selfReviewFindings = []selfReviewFinding{
	{
		regex:   regexp.MustCompile(`(?i)(?:missing|no|lack(?:ing)?)\s+error\s+handl`),
		pType:   PatternTypeError,
		title:   "Missing error handling",
		desc:    "Errors must be checked and propagated, not silently ignored",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)(?:unused\s+import|dead\s+code|unreachable\s+code)`),
		pType:   PatternTypeCode,
		title:   "Dead code detected",
		desc:    "Remove unused imports, unreachable code, and dead code paths",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)(?:config\s+field\s+not\s+wired|struct\s+field\s+unused|field\s+.*not\s+(?:used|wired|connected))`),
		pType:   PatternTypeStructure,
		title:   "Unwired config or struct field",
		desc:    "All config and struct fields must be wired through to their consumers",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)(?:test\s+coverage\s+gap|missing\s+test|no\s+test|lint\s+violation|linter\s+error)`),
		pType:   PatternTypeWorkflow,
		title:   "Test or lint gap",
		desc:    "All new code paths must have test coverage; lint violations must be resolved",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)(?:build\s+(?:fail|verification\s+fail)|compilation?\s+(?:error|fail))`),
		pType:   PatternTypeError,
		title:   "Build verification failure",
		desc:    "Code must compile and pass build verification before submission",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)(?:cross.file\s+parity|PARITY_GAP|parity\s+(?:issue|mismatch))`),
		pType:   PatternTypeStructure,
		title:   "Cross-file parity issue",
		desc:    "Related changes across files must stay in sync (interfaces, implementations, tests)",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)REVIEW_FIXED:`),
		pType:   PatternTypeCode,
		title:   "Self-review fix applied",
		desc:    "Issue caught and fixed during self-review pass",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)SCOPE_CREEP:\s*\S+\s*—`),
		pType:   PatternTypeStructure,
		title:   "Scope creep detected",
		desc:    "Change touches symbols outside the task scope; keep edits surgical",
		context: "Self-review",
	},
	{
		// Captures the tier (blocker/must/nice) so callers can route by severity.
		regex:   regexp.MustCompile(`(?i)STANDARD_VIOLATION:\s*(blocker|must|nice)\s+\S+\s*—\s*\S+`),
		pType:   PatternTypeWorkflow,
		title:   "Coding standard violation",
		desc:    "Code violates a project coding standard; resolve before submission",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)SUSPICIOUS_VALUE`),
		pType:   PatternTypeCode,
		title:   "Suspicious value detected",
		desc:    "Hardcoded or suspicious values should be verified against requirements",
		context: "Self-review",
	},
	{
		regex:   regexp.MustCompile(`(?i)INCOMPLETE`),
		pType:   PatternTypeWorkflow,
		title:   "Incomplete implementation",
		desc:    "Implementation is missing required functionality or TODO items remain",
		context: "Self-review",
	},
}

// ExtractFromSelfReview extracts patterns from self-review output by parsing
// finding markers (REVIEW_FIXED:, SUSPICIOUS_VALUE, PARITY_GAP, INCOMPLETE, etc.)
// and mapping them to pattern types. Patterns are stored as anti-patterns with
// confidence 0.5 (boosted on recurrence via existing SaveCrossPattern upsert logic).
func (e *PatternExtractor) ExtractFromSelfReview(ctx context.Context, selfReviewOutput string, projectPath string) (*ExtractionResult, error) {
	result := &ExtractionResult{
		ExecutionID:  fmt.Sprintf("self_review_%d", time.Now().UnixNano()),
		ProjectPath:  projectPath,
		Patterns:     make([]*ExtractedPattern, 0),
		AntiPatterns: make([]*ExtractedPattern, 0),
		ExtractedAt:  time.Now(),
	}

	if strings.TrimSpace(selfReviewOutput) == "" {
		return result, nil
	}

	for _, finding := range selfReviewFindings {
		matches := finding.regex.FindAllString(selfReviewOutput, -1)
		if len(matches) == 0 {
			continue
		}

		// STANDARD_VIOLATION markers carry a severity tier in the first capture
		// group; surface it on the result for severity-based routing.
		if result.Tier == "" {
			if sub := finding.regex.FindStringSubmatch(selfReviewOutput); len(sub) > 1 {
				result.Tier = strings.ToLower(sub[1])
			}
		}

		examples := append([]string{}, matches...)

		result.AntiPatterns = append(result.AntiPatterns, &ExtractedPattern{
			Type:        finding.pType,
			Title:       finding.title,
			Description: finding.desc,
			Examples:    examples,
			Confidence:  0.5,
			Context:     finding.context,
		})
	}

	return result, nil
}

// tierToConfidence maps a STANDARD_VIOLATION severity tier to a baseline
// confidence floor for the saved anti-pattern. Higher-severity tiers carry a
// stronger signal so they survive decay and surface ahead of nice-to-haves.
// Returns 0 for an absent or unknown tier (no influence on confidence).
func tierToConfidence(tier string) float64 {
	switch tier {
	case "blocker":
		return 0.9
	case "must":
		return 0.75
	case "nice":
		return 0.5
	default:
		return 0
	}
}
