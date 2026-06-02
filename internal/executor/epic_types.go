package executor

import (
	"fmt"
	"regexp"
	"strings"
)

// conventionalCommitPrefixRegex matches conventional-commit type prefixes like
// "fix:", "feat(scope):", "chore(epic):" at the start of a title.
var conventionalCommitPrefixRegex = regexp.MustCompile(`^(?i)(fix|feat|chore|docs|test|refactor|style|perf|ci|build|revert)(\([^)]+\))?:`)

// subtaskActionVerbs is the allow-list of first words that identify a subtask
// title as an action item (vs LLM analysis/prose). Kept intentionally broad to
// cover normal engineering verbs without admitting analysis sentences.
var subtaskActionVerbs = map[string]bool{
	"add": true, "adjust": true, "allow": true, "apply": true, "audit": true,
	"block": true, "build": true, "bump": true, "cache": true, "check": true,
	"clean": true, "cleanup": true, "clear": true, "consolidate": true,
	"convert": true, "create": true, "decouple": true, "dedupe": true,
	"delete": true, "deploy": true, "deprecate": true, "detect": true,
	"disable": true, "document": true, "drop": true, "emit": true, "enable": true,
	"enforce": true, "ensure": true, "expose": true, "extract": true,
	"fallback": true, "filter": true, "fix": true, "gate": true, "generate": true,
	"guard": true, "handle": true, "harden": true, "hide": true, "implement": true,
	"improve": true, "init": true, "inject": true, "install": true,
	"instrument": true, "introduce": true, "invalidate": true, "limit": true,
	"load": true, "log": true, "make": true, "merge": true, "migrate": true,
	"move": true, "normalize": true, "parse": true, "patch": true, "persist": true,
	"plumb": true, "port": true, "prefix": true, "prevent": true, "propagate": true,
	"protect": true, "provide": true, "publish": true, "refactor": true,
	"register": true, "reject": true, "remove": true, "rename": true,
	"replace": true, "reset": true, "restore": true, "retry": true, "return": true,
	"revert": true, "rewrite": true, "route": true, "sanitize": true, "scope": true,
	"seed": true, "send": true, "serialize": true, "set": true, "setup": true,
	"simplify": true, "skip": true, "split": true, "standardize": true,
	"stop": true, "store": true, "strip": true, "support": true, "surface": true,
	"switch": true, "sync": true, "teach": true, "test": true, "throttle": true,
	"trim": true, "truncate": true, "unify": true, "unwire": true, "update": true,
	"upgrade": true, "use": true, "validate": true, "verify": true, "wait": true,
	"warn": true, "wire": true, "wrap": true, "write": true,
}

// subtaskProseIndicators are phrases that strongly signal a subtask "title" is
// actually LLM analysis/prose rather than an action item. See GH-2324 / GH-2315.
var subtaskProseIndicators = []string{
	", not ", " but ", " however", "however,",
	"appears correct", "appears to ", "looks good", "looks correct",
	" is fine", " is correct", " is actually ", " already ",
	"already marks", "already handles", "already does",
	" seems to ", " seems like", " should already",
	"the status appears", "the current code",
}

// propagatableLabelAllowlist contains exact label names that survive parent→child
// propagation during epic decomposition.
var propagatableLabelAllowlist = map[string]struct{}{
	"no-decompose": {},
	"no-plan":      {},
}

// propagatableLabelPrefixes are prefix patterns whose matching labels survive propagation.
var propagatableLabelPrefixes = []string{"area:", "priority:", "scope:"}

// alwaysBlockedLabels are pilot lifecycle markers that must never propagate to sub-issues.
var alwaysBlockedLabels = map[string]struct{}{
	"pilot":                     {},
	"pilot-done":                {},
	"pilot-failed":              {},
	"pilot-in-progress":         {},
	"pilot-superseded":          {},
	"pilot-needs-clarification": {},
}

// filterPropagatableLabels returns the subset of parent labels that sub-issues
// should inherit during epic decomposition. It lowercases and trims each label,
// drops empties, drops lifecycle labels, and keeps anything in the allow-list or
// matching a propagatable prefix.
func filterPropagatableLabels(parentLabels []string) []string {
	out := make([]string, 0, len(parentLabels))
	for _, raw := range parentLabels {
		l := strings.ToLower(strings.TrimSpace(raw))
		if l == "" {
			continue
		}
		if _, blocked := alwaysBlockedLabels[l]; blocked {
			continue
		}
		if _, ok := propagatableLabelAllowlist[l]; ok {
			out = append(out, l)
			continue
		}
		for _, p := range propagatableLabelPrefixes {
			if strings.HasPrefix(l, p) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// validateSubtaskTitle reports an error when a subtask title extracted from LLM
// planning output is structurally unsuitable for use as a GitHub issue title.
//
// Incident (GH-2324): the decomposition of GH-2314 produced GH-2315 with this
// as its title, directly from the LLM's skeptical analysis of the parent issue:
//
//	"Dispatcher `recoverStaleTasks()` (line 188) already marks orphans as
//	 `\"failed\"`, not `\"completed\"`. The status appears correct in the
//	 current code."
//
// The string flowed verbatim into the sub-issue title, PR #2317 title, the
// squash-merge commit subject, and the public v2.95.3 changelog. This validator
// rejects such titles before they reach the tracker.
//
// Rejection criteria:
//  1. Contains prose/analysis indicators (", not ", " but ", "appears correct",
//     "already", ...).
//  2. Exceeds 15 words — real action titles are terse.
//  3. First significant word is neither a conventional-commit type prefix nor
//     an allow-listed action verb.
func validateSubtaskTitle(title string) error {
	t := strings.TrimSpace(title)
	if t == "" {
		return fmt.Errorf("empty title")
	}

	lower := strings.ToLower(t)
	for _, ind := range subtaskProseIndicators {
		if strings.Contains(lower, ind) {
			return fmt.Errorf("title contains analysis/prose indicator %q", strings.TrimSpace(ind))
		}
	}

	words := strings.Fields(t)
	if len(words) > 15 {
		return fmt.Errorf("title has %d words (>15); action titles should be terse", len(words))
	}

	if conventionalCommitPrefixRegex.MatchString(t) {
		return nil
	}

	firstWord := strings.ToLower(strings.Trim(words[0], "*_`\"'.,:;()[]"))
	if firstWord == "" {
		return fmt.Errorf("title has no leading word")
	}
	if !subtaskActionVerbs[firstWord] {
		return fmt.Errorf("title does not start with an action verb or conventional-commit prefix (got %q)", firstWord)
	}
	return nil
}

// syntheticSubtaskTitle builds a fallback title for subtasks whose LLM-produced
// title failed validateSubtaskTitle. Uses the parent ID so the sub-issue is
// still traceable back to the epic. GH-2324.
func syntheticSubtaskTitle(parent *Task, order int) string {
	parentID := "epic"
	if parent != nil && parent.ID != "" {
		parentID = parent.ID
	}
	return fmt.Sprintf("%s: Subtask %d", parentID, order)
}

// HasNoPlanKeyword checks whether the task title or description contains the [no-plan]
// keyword, allowing users to bypass epic planning (GH-1687).
func HasNoPlanKeyword(task *Task) bool {
	return strings.Contains(strings.ToLower(task.Title), strings.ToLower(NoPlanKeyword)) ||
		strings.Contains(strings.ToLower(task.Description), strings.ToLower(NoPlanKeyword))
}

// EpicPlan represents the result of planning an epic task.
// Contains the parent task and the subtasks derived from Claude Code's planning output.
type EpicPlan struct {
	// ParentTask is the original epic task that was planned
	ParentTask *Task

	// Subtasks are the sequential subtasks derived from the planning phase
	Subtasks []PlannedSubtask

	// TotalEffort is the estimated total effort (if provided by the planner)
	TotalEffort string

	// PlanOutput is the raw Claude Code output for reference
	PlanOutput string
}

// PlannedSubtask represents a single subtask derived from epic planning.
type PlannedSubtask struct {
	// Title is the short title of the subtask
	Title string

	// Description is the detailed description of what needs to be done
	Description string

	// Order is the execution order (1-indexed)
	Order int

	// DependsOn contains the orders of subtasks this depends on
	DependsOn []int
}

// CreatedIssue represents an issue created from a planned subtask.
// Supports both GitHub (numeric Number) and other trackers (string Identifier).
type CreatedIssue struct {
	// Number is the GitHub issue number (0 for non-GitHub adapters)
	Number int

	// Identifier is the issue identifier string (GH-1471).
	// For GitHub: same as Number as string (e.g., "123")
	// For Linear: full identifier (e.g., "APP-123")
	// For Jira: issue key (e.g., "PROJ-456")
	// This field is always populated; Number is for backwards compatibility.
	Identifier string

	// URL is the full issue URL
	URL string

	// State is the issue state ("open" or "closed") populated by recoverExistingSubIssues.
	State string

	// Subtask is the planned subtask this issue was created from
	Subtask PlannedSubtask
}
