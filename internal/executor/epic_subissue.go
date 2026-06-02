package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// conventionalSubtaskTitleRE mirrors the conventional-commit regex from the github
// package but is defined here to avoid an import cycle (adapters/github → executor).
var conventionalSubtaskTitleRE = regexp.MustCompile(
	`^(feat|fix|chore|refactor|test|docs|perf|build|ci|style)(\([^)]+\))?: .+$`,
)

// placeholderSubtaskTitleRE matches synthetic fallback titles like "GH-123: Subtask 1"
// produced by syntheticSubtaskTitle. Their presence in a batch signals a re-prompt is needed.
var placeholderSubtaskTitleRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*-\d+:\s+Subtask\s+\d+$`)

// parentTypeScopeRE extracts the conventional-commit prefix from a parent task title.
// Used by Approach B fallback: "feat(auth):" → "feat(auth): " prepended to the subtask description.
var parentTypeScopeRE = regexp.MustCompile(
	`^(feat|fix|chore|refactor|test|docs|perf|build|ci|style)(\([^)]+\))?:`,
)

// ErrSubIssuesAlreadyExist is returned by CreateSubIssues when open sub-issues
// referencing the parent already exist, preventing duplicate batch creation.
var ErrSubIssuesAlreadyExist = errors.New("open sub-issues already exist for this parent")

// ErrParentDone is returned by CreateSubIssues when the parent task is already
// closed or skipped, so spawning sub-issues would be wasteful (GH-2867).
var ErrParentDone = errors.New("parent task is already done; refusing to create sub-issues")

// isParentDone reports whether a task should be treated as done based on its
// labels (pilot-done, pilot-skip) or its state (closed, merged).
//
// Defensive fallback: whenever `State` is empty and the task ID looks like a
// GitHub issue ("GH-N"), this function shells out to `gh issue view` to fetch
// the authoritative state. This catches every code path that produces a Task
// without `State`:
//
//   - The dispatcher worker (`dispatcher.go:639`) reconstructs Task from a
//     persisted execution row; the `executions` schema has no `task_state`
//     column.
//   - The `task_labels` column may be populated but stale (frozen at queue
//     time) — a parent queued with `["pilot"]` and later closed with
//     `pilot-done` produces a Task whose labels still say `["pilot"]`.
//
// Both cases bypassed the gate during the 2026-05-08 GH-201 incident
// (70+ spurious OAuth sub-issues). Non-fatal on lookup error: returns false
// so this never blocks legitimate dispatches.
//
// Tests can override this var to assert the fallback path or to keep the
// production default no-op when constructing tasks with deterministic GH-* IDs.
var isParentDoneLiveFallback = func(taskID, dir string) bool {
	// Never shell out during `go test` — tests override this var explicitly
	// when they want to exercise the fallback path.
	if testing.Testing() {
		return false
	}
	return queryParentDoneViaGitHub(taskID, dir)
}

func isParentDone(t *Task) bool {
	if t == nil {
		return false
	}
	for _, label := range t.Labels {
		if label == "pilot-done" || label == "pilot-skip" {
			return true
		}
	}
	if t.State == "closed" || t.State == "merged" {
		return true
	}
	// Live fallback when State is missing — Labels alone are not authoritative
	// because dispatcher-restored Tasks carry stale labels from queue time.
	// We only reach here when no terminal label was found above; the remaining
	// label values are non-terminal and therefore inconclusive.
	if t.State == "" && strings.HasPrefix(t.ID, "GH-") {
		if isParentDoneLiveFallback(t.ID, t.ProjectPath) {
			return true
		}
	}
	return false
}

// queryParentDoneViaGitHub returns true when the GitHub issue identified by
// taskID ("GH-N") is closed/merged or carries pilot-done / pilot-skip labels.
// Non-fatal: any subprocess or parse error returns false so the caller falls
// back to the default decision.
func queryParentDoneViaGitHub(taskID, dir string) bool {
	issueNum := strings.TrimPrefix(taskID, "GH-")
	if issueNum == "" || issueNum == taskID {
		return false
	}
	args := []string{"issue", "view", issueNum, "--json", "state,labels"}
	cmd := exec.Command("gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	var resp struct {
		State  string `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(resp.State))
	if state == "closed" || state == "merged" {
		return true
	}
	for _, l := range resp.Labels {
		switch strings.ToLower(strings.TrimSpace(l.Name)) {
		case "pilot-done", "pilot-skip":
			return true
		}
	}
	return false
}

// isConventionalSubtaskTitle reports whether title is in conventional-commits format.
func isConventionalSubtaskTitle(title string) bool {
	return conventionalSubtaskTitleRE.MatchString(strings.TrimSpace(title))
}

// isPlaceholderSubtaskTitle reports whether title is a synthetic placeholder like "GH-123: Subtask 1".
func isPlaceholderSubtaskTitle(title string) bool {
	return placeholderSubtaskTitleRE.MatchString(strings.TrimSpace(title))
}

// scopeKeywords returns body keywords that confirm a conventional-commit scope is legit.
// Only covers scopes that have caused cascade contamination; all others return nil (always trusted).
func scopeKeywords(scope string) []string {
	switch scope {
	case "auth":
		return []string{"auth", "oauth", "login", "token", "session", "identity", "credential", "password", "jwt"}
	default:
		return nil
	}
}

// extractScope pulls the scope name out of a conventional-commit prefix like "feat(auth):".
// Returns "" when the prefix has no scope (e.g. "fix:").
func extractScope(prefix string) string {
	start := strings.Index(prefix, "(")
	end := strings.Index(prefix, ")")
	if start < 0 || end < 0 || end <= start+1 {
		return ""
	}
	return prefix[start+1 : end]
}

// isCascadeArtefact returns true when the parent title carries a scoped prefix but
// the parent body contains no keywords matching that scope — a signal that the title
// was inherited from an unrelated earlier ticket (cascade-artefact pattern, GH-2587).
func isCascadeArtefact(prefix, body string) bool {
	scope := extractScope(prefix)
	if scope == "" {
		return false // no scope to verify
	}
	keywords := scopeKeywords(scope)
	if keywords == nil {
		return false // scope not in watchlist — trust it
	}
	bodyLower := strings.ToLower(body)
	for _, kw := range keywords {
		if strings.Contains(bodyLower, kw) {
			return false // body confirms scope is legit
		}
	}
	return true // title claims scope X but body never mentions X
}

// extractParentTypeScope returns the conventional-commit prefix (e.g. "feat(auth):") from
// parentTitle. Falls back to "chore:" when the parent title is not in conventional-commit format
// or when the scoped prefix looks like a cascade artefact (GH-2587: title scope not reflected in body).
func extractParentTypeScope(parentTitle, parentBody string) string {
	m := parentTypeScopeRE.FindString(strings.TrimSpace(parentTitle))
	if m == "" {
		return "chore:"
	}
	if isCascadeArtefact(m, parentBody) {
		return "chore:"
	}
	return m
}

// applyParentTypeScopeFallback rewrites the subtasks at invalidIdx using the parent's
// conventional-commit prefix (Approach B). The result is guaranteed to satisfy
// conventionalSubtaskTitleRE.
func applyParentTypeScopeFallback(subtasks []PlannedSubtask, invalidIdx []int, parentTitle, parentBody string) []PlannedSubtask {
	prefix := extractParentTypeScope(parentTitle, parentBody) // e.g. "feat(auth):"
	result := make([]PlannedSubtask, len(subtasks))
	copy(result, subtasks)
	for _, idx := range invalidIdx {
		if idx < 0 || idx >= len(result) {
			continue
		}
		st := &result[idx]
		raw := strings.TrimSpace(st.Title)
		// Strip any existing issue-id prefix like "GH-N: "
		raw = issuePrefixRegex.ReplaceAllString(raw, "")
		// Replace bare "Subtask N" placeholders with a meaningful verb phrase
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "subtask") {
			raw = fmt.Sprintf("implement subtask %d", st.Order)
		}
		st.Title = prefix + " " + lowercaseFirstRune(raw)
	}
	return result
}

// findInvalidSubtaskTitleIdx returns the indices of subtasks whose titles are either
// not in conventional-commits format or are synthetic placeholder strings.
func findInvalidSubtaskTitleIdx(subtasks []PlannedSubtask) []int {
	var invalid []int
	for i, st := range subtasks {
		if isPlaceholderSubtaskTitle(st.Title) || !isConventionalSubtaskTitle(st.Title) {
			invalid = append(invalid, i)
		}
	}
	return invalid
}

// validateAndFixSubtaskTitles ensures every subtask title follows conventional-commits
// format (type(scope): description). Invalid or placeholder titles trigger a re-prompt
// via SubtaskParser; if the re-prompt fails or no parser is available, Approach B
// inherits the parent's type/scope prefix.
func validateAndFixSubtaskTitles(ctx context.Context, subtasks []PlannedSubtask, parent *Task, parser *SubtaskParser, log *slog.Logger) []PlannedSubtask {
	invalid := findInvalidSubtaskTitleIdx(subtasks)
	if len(invalid) == 0 {
		return subtasks
	}

	parentTitle := ""
	parentBody := ""
	if parent != nil {
		parentTitle = parent.Title
		parentBody = parent.Description
	}

	// Attempt re-prompt via SubtaskParser API.
	if parser != nil {
		invalidSubtasks := make([]PlannedSubtask, 0, len(invalid))
		for _, idx := range invalid {
			invalidSubtasks = append(invalidSubtasks, subtasks[idx])
		}

		reformatted, err := parser.ReformatTitles(ctx, parentTitle, invalidSubtasks)
		if err == nil && len(reformatted) > 0 {
			// Merge reformatted titles back by order.
			byOrder := make(map[int]string, len(reformatted))
			for _, st := range reformatted {
				byOrder[st.Order] = st.Title
			}
			updated := make([]PlannedSubtask, len(subtasks))
			copy(updated, subtasks)
			for _, idx := range invalid {
				if t, ok := byOrder[updated[idx].Order]; ok && t != "" {
					updated[idx].Title = t
				}
			}
			// Re-check: if all valid after re-prompt, done.
			if len(findInvalidSubtaskTitleIdx(updated)) == 0 {
				return updated
			}
			// Partial fix — use the updated slice as base for Approach B.
			subtasks = updated
			invalid = findInvalidSubtaskTitleIdx(subtasks)
		} else if log != nil {
			log.Warn("subtask title re-prompt failed; applying Approach B fallback",
				"invalid_count", len(invalid),
				"error", err,
			)
		}
	}

	// Approach B: inherit parent's type/scope for remaining invalid titles.
	return applyParentTypeScopeFallback(subtasks, invalid, parentTitle, parentBody)
}
