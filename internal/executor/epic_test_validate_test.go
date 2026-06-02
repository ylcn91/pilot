package executor

import (
	"context"
	"strings"
	"testing"
)

// TestValidateSubtaskTitle covers GH-2324: rejecting LLM analysis-style titles
// before they become sub-issue titles / PR titles / commit subjects.
func TestValidateSubtaskTitle(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		wantError bool
	}{
		// GH-2315 incident — the exact string that flowed into commit 70c14dc5.
		{
			name:      "GH-2315 incident title is rejected",
			title:     "Dispatcher `recoverStaleTasks()` (line 188) already marks orphans as `\"failed\"`, not `\"completed\"`. The status appears correct in the current code.",
			wantError: true,
		},
		{
			name:      "analysis clause with 'already' is rejected",
			title:     "Dispatcher already marks orphans as failed",
			wantError: true,
		},
		{
			name:      "contrast clause 'not X' is rejected",
			title:     "Adds a label, not a comment, on completion",
			wantError: true,
		},
		{
			name:      "'appears correct' evaluative phrase is rejected",
			title:     "Handler appears correct in current code",
			wantError: true,
		},
		{
			name:      "too many words is rejected",
			title:     "Add a function that takes a parameter and returns a value and also handles errors in a nice way always",
			wantError: true,
		},
		{
			name:      "first word is a noun, not an action verb, is rejected",
			title:     "Dispatcher recovery semantics",
			wantError: true,
		},
		{
			name:      "empty title is rejected",
			title:     "   ",
			wantError: true,
		},

		// Positive cases — realistic action-item titles that must pass.
		{
			name:      "plain action verb title passes",
			title:     "Add validateSubtaskTitle helper",
			wantError: false,
		},
		{
			name:      "conventional commit prefix passes",
			title:     "fix(epic): validate sub-issue titles",
			wantError: false,
		},
		{
			name:      "conventional commit no scope passes",
			title:     "feat: introduce sub-issue title validator",
			wantError: false,
		},
		{
			name:      "refactor verb passes",
			title:     "Refactor splitTitleDescription to handle edge cases",
			wantError: false,
		},
		{
			name:      "wire action verb passes",
			title:     "Wire validator into both create paths",
			wantError: false,
		},
		{
			name:      "terse fix title passes",
			title:     "Fix stale SHA in autopilot",
			wantError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSubtaskTitle(tc.title)
			if tc.wantError && err == nil {
				t.Errorf("validateSubtaskTitle(%q) = nil, want error", tc.title)
			}
			if !tc.wantError && err != nil {
				t.Errorf("validateSubtaskTitle(%q) = %v, want nil", tc.title, err)
			}
		})
	}
}

// TestSyntheticSubtaskTitle verifies fallback titles are deterministic and
// include the parent ID so sub-issues remain traceable to the epic.
func TestSyntheticSubtaskTitle(t *testing.T) {
	t.Run("uses parent ID when present", func(t *testing.T) {
		got := syntheticSubtaskTitle(&Task{ID: "GH-2314"}, 2)
		want := "GH-2314: Subtask 2"
		if got != want {
			t.Errorf("syntheticSubtaskTitle = %q, want %q", got, want)
		}
	})
	t.Run("uses 'epic' fallback when parent ID is empty", func(t *testing.T) {
		got := syntheticSubtaskTitle(&Task{}, 1)
		want := "epic: Subtask 1"
		if got != want {
			t.Errorf("syntheticSubtaskTitle = %q, want %q", got, want)
		}
	})
	t.Run("handles nil parent", func(t *testing.T) {
		got := syntheticSubtaskTitle(nil, 3)
		want := "epic: Subtask 3"
		if got != want {
			t.Errorf("syntheticSubtaskTitle = %q, want %q", got, want)
		}
	})
}

// TestExtractParentTypeScope_CascadeArtefactGuard covers GH-2587: when the parent title
// carries a scoped prefix (e.g. "feat(auth):") but the body has no matching keywords,
// extractParentTypeScope must fall back to "chore:" to avoid cascade contamination.
func TestExtractParentTypeScope_CascadeArtefactGuard(t *testing.T) {
	tests := []struct {
		name        string
		parentTitle string
		parentBody  string
		wantPrefix  string
	}{
		{
			// Case 1 (cascade-2 repro): GH-201 — dashboard ticket whose title coincidentally
			// started with "feat(auth):" but body was entirely about UI, not auth.
			name:        "cascade-2 repro: auth prefix with unrelated body returns chore",
			parentTitle: "feat(auth): dashboard sparkline retro",
			parentBody:  "add sparkline cards for cost panel — see attached design",
			wantPrefix:  "chore:",
		},
		{
			// Case 2 (legit auth feature): body mentions auth-related keywords.
			name:        "legit auth feature: body mentions oauth returns feat(auth):",
			parentTitle: "feat(auth): add OAuth provider integration",
			parentBody:  "Implement OAuth login flow using GitHub provider tokens for session management",
			wantPrefix:  "feat(auth):",
		},
		{
			// Case 3 (no scope): title has no scope — scope check doesn't apply; prefix kept as-is.
			name:        "no scope: fix: prefix preserved regardless of body",
			parentTitle: "fix: timeout in retry path",
			parentBody:  "the retry loop does not honour context cancellation",
			wantPrefix:  "fix:",
		},
		{
			// Additional: scope not in watchlist — always trusted.
			name:        "executor scope not in watchlist: trusted",
			parentTitle: "feat(executor): add stream-json parser",
			parentBody:  "completely unrelated body about dashboard widgets",
			wantPrefix:  "feat(executor):",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractParentTypeScope(tc.parentTitle, tc.parentBody)
			if got != tc.wantPrefix {
				t.Errorf("extractParentTypeScope(%q, ...) = %q, want %q", tc.parentTitle, got, tc.wantPrefix)
			}
		})
	}
}

// TestApplyParentTypeScopeFallback_CascadeGuardEndToEnd verifies that the cascade-artefact
// guard flows through applyParentTypeScopeFallback: subtasks with invalid titles under a
// cascade-artefact parent must receive "chore:" not the contaminated scope.
func TestApplyParentTypeScopeFallback_CascadeGuardEndToEnd(t *testing.T) {
	subtasks := []PlannedSubtask{
		{Title: "Subtask 1", Order: 1},
		{Title: "fix: valid title already", Order: 2},
		{Title: "Subtask 3", Order: 3},
	}
	invalid := []int{0, 2}

	// Cascade-artefact parent: auth prefix but body is about dashboard sparklines.
	result := applyParentTypeScopeFallback(subtasks, invalid, "feat(auth): dashboard sparkline retro", "add sparkline cards for cost panel")

	for _, idx := range invalid {
		if !isConventionalSubtaskTitle(result[idx].Title) {
			t.Errorf("result[%d].Title %q is not conventional-commit format", idx, result[idx].Title)
		}
		if strings.HasPrefix(result[idx].Title, "feat(auth):") {
			t.Errorf("result[%d].Title %q must not inherit cascade-artefact auth scope", idx, result[idx].Title)
		}
		if !strings.HasPrefix(result[idx].Title, "chore:") {
			t.Errorf("result[%d].Title %q should use chore: fallback, not %q", idx, result[idx].Title, result[idx].Title[:strings.Index(result[idx].Title, " ")+1])
		}
	}

	// Untouched slot must be preserved.
	if result[1].Title != "fix: valid title already" {
		t.Errorf("untouched subtask title changed: got %q", result[1].Title)
	}
}

// TestCreateSubIssues_RejectsAnalysisTitle ensures the GH-2324 guard kicks in
// end-to-end through the adapter path: an LLM analysis sentence must not reach
// the tracker verbatim; a synthetic fallback is used instead.
func TestCreateSubIssues_RejectsAnalysisTitle(t *testing.T) {
	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)

	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-101", URL: "https://linear.app/test/issue/APP-101"},
		},
	}
	runner.SetSubIssueCreator(mock)

	// Exact incident string from GH-2315.
	badTitle := "Dispatcher `recoverStaleTasks()` (line 188) already marks orphans as `\"failed\"`, not `\"completed\"`. The status appears correct in the current code."

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-100",
			SourceAdapter: "linear",
			SourceIssueID: "APP-100",
			Title:         "Parent epic",
		},
		Subtasks: []PlannedSubtask{
			{Title: badTitle, Description: "body", Order: 1},
		},
	}

	ctx := context.Background()
	if _, err := runner.CreateSubIssues(ctx, plan, ""); err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}

	if len(mock.Called) != 1 {
		t.Fatalf("expected 1 adapter call, got %d", len(mock.Called))
	}
	got := mock.Called[0].Title
	if got == badTitle {
		t.Errorf("analysis-style title was passed through unchanged: %q", got)
	}
	// Fallback must be a valid conventional-commit title (not a placeholder).
	if !isConventionalSubtaskTitle(got) {
		t.Errorf("fallback title %q is not in conventional-commit format", got)
	}
	if isPlaceholderSubtaskTitle(got) {
		t.Errorf("fallback title %q must not be a placeholder like 'GH-N: Subtask K'", got)
	}
}
