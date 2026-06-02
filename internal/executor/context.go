package executor

import (
	"context"
	"strings"
	"sync"

	"github.com/ylcn91/pilot/internal/memory"
)

// PatternContext provides learned patterns for task execution
type PatternContext struct {
	queryService *memory.PatternQueryService

	// applied tracks the IDs of patterns actually injected for a given task
	// context so that post-execution outcome recording can credit only the
	// patterns that were surfaced, not every pattern linked to the project
	// (B3 — applied-pattern ID precision). Keyed by appliedKey(...).
	appliedMu sync.Mutex
	applied   map[string][]string
}

// NewPatternContext creates a new pattern context provider
func NewPatternContext(store *memory.Store) *PatternContext {
	return &PatternContext{
		queryService: memory.NewPatternQueryService(store),
		applied:      make(map[string][]string),
	}
}

// GetPatternsForTask retrieves relevant patterns for a task
func (c *PatternContext) GetPatternsForTask(ctx context.Context, projectPath, taskType, taskDescription string) (string, error) {
	return c.queryService.FormatForPrompt(ctx, projectPath, taskType, taskDescription)
}

// appliedKey derives the tracking key for a task context. It is reconstructible
// from a *Task at outcome-recording time (same project/type/description used at
// injection), so injected IDs survive the gap between prompt build and learning.
func appliedKey(projectPath, taskType, taskDescription string) string {
	return projectPath + "\x00" + taskType + "\x00" + taskDescription
}

// AppliedPatterns returns the IDs of patterns that were injected for the given
// task context, or nil if none were injected. The record is consumed (cleared)
// so repeated executions of the same context don't read stale IDs.
func (c *PatternContext) AppliedPatterns(projectPath, taskType, taskDescription string) []string {
	c.appliedMu.Lock()
	defer c.appliedMu.Unlock()
	key := appliedKey(projectPath, taskType, taskDescription)
	ids := c.applied[key]
	delete(c.applied, key)
	return ids
}

// InjectPatterns adds learned patterns to a prompt. The IDs of the patterns it
// injects are recorded internally (keyed by task context) so post-execution
// outcome recording can credit only the injected patterns — not every pattern
// linked to the project — via AppliedPatterns (B3 — applied-pattern precision).
//
// The signature is intentionally unchanged (no IDs returned) because the only
// callers live in prompt_builder.go, which is owned by a parallel workstream;
// see the WS5 blockers note. The internal record bridges the gap instead.
func (c *PatternContext) InjectPatterns(ctx context.Context, prompt, projectPath, taskType, taskDescription string) (string, error) {
	patterns, err := c.GetPatternsForTask(ctx, projectPath, taskType, taskDescription)
	if err != nil {
		// Don't fail task if pattern injection fails, just log
		return prompt, nil
	}

	if patterns == "" {
		return prompt, nil
	}

	c.recordApplied(projectPath, taskType, taskDescription,
		c.injectedPatternIDs(ctx, projectPath, taskType, taskDescription))

	// Insert patterns before the task description
	// Find "## Task:" marker and insert before it
	taskMarker := "## Task:"
	idx := strings.Index(prompt, taskMarker)
	if idx != -1 {
		var sb strings.Builder
		sb.WriteString(prompt[:idx])
		sb.WriteString(patterns)
		sb.WriteString("\n")
		sb.WriteString(prompt[idx:])
		return sb.String(), nil
	}

	// No marker found, prepend patterns
	return patterns + "\n" + prompt, nil
}

// injectedPatternIDs mirrors the recommended-pattern selection that
// FormatForPrompt renders, returning the IDs of the non-anti patterns that were
// surfaced. Anti-patterns are excluded — they are warnings, not applied work.
func (c *PatternContext) injectedPatternIDs(ctx context.Context, projectPath, taskType, taskDescription string) []string {
	patterns, err := c.queryService.GetRelevantPatterns(ctx, projectPath, taskType, taskDescription)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if p.IsAntiPattern {
			continue
		}
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func (c *PatternContext) recordApplied(projectPath, taskType, taskDescription string, ids []string) {
	if len(ids) == 0 {
		return
	}
	c.appliedMu.Lock()
	c.applied[appliedKey(projectPath, taskType, taskDescription)] = ids
	c.appliedMu.Unlock()
}

// PatternContextConfig configures pattern context injection
type PatternContextConfig struct {
	Enabled       bool    // Enable pattern injection
	MinConfidence float64 // Minimum confidence for patterns
	MaxPatterns   int     // Maximum patterns to inject
	IncludeAnti   bool    // Include anti-patterns
}

// inferTaskType derives a task type string from the task title or labels.
// Returns a short label like "feat", "fix", "refactor", "test", "docs", "chore".
func inferTaskType(task *Task) string {
	title := strings.ToLower(task.Title)
	for _, prefix := range []string{"feat", "fix", "refactor", "test", "docs", "chore"} {
		if strings.HasPrefix(title, prefix) {
			return prefix
		}
	}
	for _, label := range task.Labels {
		l := strings.ToLower(label)
		for _, t := range []string{"feat", "fix", "refactor", "test", "docs", "chore", "bug", "enhancement"} {
			if l == t {
				if l == "bug" {
					return "fix"
				}
				if l == "enhancement" {
					return "feat"
				}
				return l
			}
		}
	}
	return "feat" // default
}

// DefaultPatternContextConfig returns default configuration
func DefaultPatternContextConfig() *PatternContextConfig {
	return &PatternContextConfig{
		Enabled:       true,
		MinConfidence: 0.6,
		MaxPatterns:   5,
		IncludeAnti:   true,
	}
}
