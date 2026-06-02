package executor

import (
	"context"
	"regexp"
	"strings"
)

// DecomposeConfig configures auto-decomposition of complex tasks.
type DecomposeConfig struct {
	// Enabled controls whether auto-decomposition is active.
	Enabled bool `yaml:"enabled"`

	// MinComplexity is the minimum complexity level that triggers decomposition.
	// Valid values: "complex" (default). Only complex tasks are decomposed.
	MinComplexity string `yaml:"min_complexity"`

	// MaxSubtasks limits the number of subtasks created from decomposition.
	// Default: 5. Range: 2-10.
	MaxSubtasks int `yaml:"max_subtasks"`

	// MinDescriptionWords is the minimum word count in description to trigger decomposition.
	// Tasks with fewer words are not decomposed even if complex.
	// Default: 50.
	MinDescriptionWords int `yaml:"min_description_words"`
}

// DefaultDecomposeConfig returns default decomposition settings.
func DefaultDecomposeConfig() *DecomposeConfig {
	return &DecomposeConfig{
		Enabled:             false, // Disabled by default, opt-in
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 50,
	}
}

// DecomposeResult contains the outcome of task decomposition.
type DecomposeResult struct {
	// Decomposed indicates whether the task was split.
	Decomposed bool

	// Subtasks contains the generated subtasks (empty if not decomposed).
	Subtasks []*Task

	// Reason explains why decomposition did or did not occur.
	Reason string
}

// NoDecomposeLabel is the GitHub label that bypasses decomposition entirely (GH-664).
const NoDecomposeLabel = "no-decompose"

// NoPlanKeyword is a keyword that users can include in the task title or description
// to bypass epic planning and decomposition (GH-1687).
const NoPlanKeyword = "[no-plan]"

// noDecomposePhrases are precompiled patterns that signal a task must not be split.
// Matched against lowercased title + description (GH-2783).
var noDecomposePhrases = []*regexp.Regexp{
	regexp.MustCompile(`single ac list`),
	regexp.MustCompile(`do not decompose`),
	regexp.MustCompile(`do not split`),
	regexp.MustCompile(`single pilot issue`),
	regexp.MustCompile(`keep as .+ single`),
	regexp.MustCompile(`splitting this would`),
	regexp.MustCompile(`<!--\s*pilot:no-decompose\s*-->`),
}

// HasNoDecomposePhrase returns true when the task title or description contains
// prose that signals the task must not be split into subtasks (GH-2783).
func HasNoDecomposePhrase(task *Task) bool {
	haystack := strings.ToLower(task.Title + " " + task.Description)
	for _, re := range noDecomposePhrases {
		if re.MatchString(haystack) {
			return true
		}
	}
	return false
}

// TaskDecomposer handles breaking complex tasks into smaller subtasks.
type TaskDecomposer struct {
	config     *DecomposeConfig
	classifier *ComplexityClassifier // Optional LLM classifier (GH-727); nil = use heuristic
}

// NewTaskDecomposer creates a decomposer with the given configuration.
func NewTaskDecomposer(config *DecomposeConfig) *TaskDecomposer {
	if config == nil {
		config = DefaultDecomposeConfig()
	}
	return &TaskDecomposer{config: config}
}

// SetClassifier attaches an LLM complexity classifier to the decomposer (GH-727).
// When set, the classifier is used instead of the word-count heuristic.
func (d *TaskDecomposer) SetClassifier(c *ComplexityClassifier) {
	d.classifier = c
}

// Decompose analyzes a task and potentially splits it into subtasks.
// Returns the original task wrapped in DecomposeResult if decomposition
// is not triggered or not applicable.
func (d *TaskDecomposer) Decompose(task *Task) *DecomposeResult {
	return d.DecomposeWithContext(context.Background(), task)
}

// DecomposeWithContext is like Decompose but accepts a context for the LLM call.
func (d *TaskDecomposer) DecomposeWithContext(ctx context.Context, task *Task) *DecomposeResult {
	if task == nil {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   nil,
			Reason:     "nil task",
		}
	}

	// Check if decomposition is enabled
	if !d.config.Enabled {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "decomposition disabled",
		}
	}

	// GH-664: Skip decomposition entirely if task has no-decompose label
	if HasLabel(task, NoDecomposeLabel) {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "skipped: no-decompose label",
		}
	}

	// GH-727: Use LLM classifier if available, otherwise fall back to heuristic
	var complexity Complexity
	if d.classifier != nil {
		complexity = d.classifier.Classify(ctx, task)
	} else {
		complexity = DetectComplexity(task)
	}

	if !d.shouldDecompose(complexity) {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "complexity below threshold: " + complexity.String(),
		}
	}

	// Check description length — only enforce in heuristic mode (GH-1728).
	// When the LLM classifier is attached and confirmed COMPLEX, trust it over word count.
	wordCount := len(strings.Fields(task.Description))
	usedLLMClassifier := d.classifier != nil
	if !usedLLMClassifier && wordCount < d.config.MinDescriptionWords {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "description too short for decomposition (heuristic mode)",
		}
	}

	// Analyze and split
	subtasks := d.analyzeAndSplit(task)
	if len(subtasks) <= 1 {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "no decomposition points found",
		}
	}

	return &DecomposeResult{
		Decomposed: true,
		Subtasks:   subtasks,
		Reason:     "decomposed into subtasks",
	}
}

// DecomposeForRetry attempts decomposition after an execution failure (OOM/killed).
// Bypasses word count gate — execution failure already proved task is too large.
// Still respects no-decompose label and requires structural split points.
// GH-1716: Safety net for tasks that slip through initial classification.
func (d *TaskDecomposer) DecomposeForRetry(ctx context.Context, task *Task) *DecomposeResult {
	if task == nil {
		return &DecomposeResult{Decomposed: false, Reason: "nil task"}
	}

	if HasLabel(task, NoDecomposeLabel) {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "skipped: no-decompose label (even on retry)",
		}
	}

	subtasks := d.analyzeAndSplit(task)
	if len(subtasks) <= 1 {
		return &DecomposeResult{
			Decomposed: false,
			Subtasks:   []*Task{task},
			Reason:     "no decomposition points found (retry fallback)",
		}
	}

	return &DecomposeResult{
		Decomposed: true,
		Subtasks:   subtasks,
		Reason:     "decomposed after execution failure (retry fallback)",
	}
}

// shouldDecompose checks if the complexity meets the threshold.
// Epic tasks are always decomposable since they're too large for single execution.
func (d *TaskDecomposer) shouldDecompose(complexity Complexity) bool {
	// Epic tasks should always be decomposed
	if complexity == ComplexityEpic {
		return true
	}
	switch d.config.MinComplexity {
	case "complex":
		return complexity == ComplexityComplex
	case "medium":
		return complexity == ComplexityComplex || complexity == ComplexityMedium
	default:
		return complexity == ComplexityComplex
	}
}
