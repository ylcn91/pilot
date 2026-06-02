package executor

import (
	"context"
	"strings"
	"testing"
)

// TestRunner_PRCreate_EmptyBranch_TriggersRetry verifies that when a branch has
// no commits relative to base, the no-commit retry path fires and gh pr create is
// never reached. The backend succeeds but makes no git commits, so the guard at
// ~line 2151 detects this, retries once, and returns no_changes when still empty.
func TestRunner_PRCreate_EmptyBranch_TriggersRetry(t *testing.T) {
	const branch = "pilot/GH-9999"
	dir := setupPRGuardRepo(t, branch, false) // no additional commits

	// Backend always succeeds but makes no git commits.
	backend := &mockFixedBackend{
		result: &BackendResult{Success: true, Output: "analysis complete"},
	}
	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true
	runner.config = &BackendConfig{SkipSelfReview: true}

	task := &Task{
		ID:          "GH-9999",
		Title:       "fix(executor): no-commits guard test",
		Description: "Verify retry fires on empty branch and CreatePR is not called",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    true,
	}

	result, err := runner.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Error("Expected failure, got success")
	}
	if !strings.HasPrefix(result.Error, "no_changes:") {
		t.Errorf("Expected no_changes error (retry path), got: %q", result.Error)
	}
	// Backend called twice: initial execution + no-commit retry (GH-916).
	// CreatePR is not called because the retry path returns early.
	backend.mu.Lock()
	count := backend.execCount
	backend.mu.Unlock()
	if count != 2 {
		t.Errorf("Expected backend called 2 times (initial + retry), got %d", count)
	}
}

// TestRunner_PRCreate_HasCommits_ProceedsToCreate verifies the GH-2743 guard
// does NOT fire when the branch has commits, allowing PR creation to proceed past
// the guard. Execution reaches the push step (which fails without a remote),
// confirming the guard was not the source of failure.
func TestRunner_PRCreate_HasCommits_ProceedsToCreate(t *testing.T) {
	const branch = "pilot/GH-9998"
	dir := setupPRGuardRepo(t, branch, true) // one commit on branch

	// Backend always succeeds; the pre-made commit satisfies the no-commit check.
	backend := &mockFixedBackend{
		result: &BackendResult{Success: true, Output: "implementation complete"},
	}
	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true
	runner.config = &BackendConfig{SkipSelfReview: true}

	task := &Task{
		ID:          "GH-9998",
		Title:       "fix(executor): no-commits guard test with commits",
		Description: "Verify guard passes and execution proceeds toward CreatePR",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    true,
	}

	result, err := runner.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Error("Expected failure (push has no remote), got success")
	}
	// Guard must NOT have fired — error must come from push or PR creation, not the guard.
	if strings.HasPrefix(result.Error, "no_changes:") {
		t.Errorf("Guard fired unexpectedly on branch with commits: %q", result.Error)
	}
	// Error comes from the push step (no remote configured), proving execution
	// proceeded past both the no-commit check and the GH-2743 guard.
	if !strings.HasPrefix(result.Error, "push failed:") {
		t.Errorf("Expected push failed error (guard passed), got: %q", result.Error)
	}
}

// TestParseDeclinedReason verifies the DECLINED:<reason> marker extraction. GH-2777.
func TestParseDeclinedReason(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantReason string
		wantFound  bool
	}{
		{
			name:       "simple marker",
			input:      "DECLINED: The module already exists in internal/auth/jwt.go.",
			wantReason: "The module already exists in internal/auth/jwt.go.",
			wantFound:  true,
		},
		{
			name:       "marker with leading prose",
			input:      "I analyzed the codebase.\nDECLINED: Requirements are too vague to implement safely.",
			wantReason: "Requirements are too vague to implement safely.",
			wantFound:  true,
		},
		{
			name:       "marker followed by trailing prose",
			input:      "DECLINED: Feature already exists.\n\nHere is more detail about why...",
			wantReason: "Feature already exists.",
			wantFound:  true,
		},
		{
			name:      "no marker — implementation text",
			input:     "I implemented the feature in internal/foo/bar.go and committed it.",
			wantFound: false,
		},
		{
			name:      "empty string",
			input:     "",
			wantFound: false,
		},
		{
			name:      "marker with empty reason",
			input:     "DECLINED:",
			wantFound: false,
		},
		{
			name:      "marker with only whitespace reason",
			input:     "DECLINED:   ",
			wantFound: false,
		},
		{
			name:       "marker without space after colon",
			input:      "DECLINED:needs-clarification",
			wantReason: "needs-clarification",
			wantFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, found := parseDeclinedReason(tt.input)
			if found != tt.wantFound {
				t.Errorf("parseDeclinedReason(%q) found=%v, want %v", tt.input, found, tt.wantFound)
			}
			if found && reason != tt.wantReason {
				t.Errorf("parseDeclinedReason(%q) reason=%q, want %q", tt.input, reason, tt.wantReason)
			}
		})
	}
}

// TestRunner_DECLINED_Path verifies that when the retry result contains a
// DECLINED:<reason> marker, result.Declined is set to true and no failure
// escalation occurs (success=false but Declined=true, not a generic no_changes). GH-2777.
func TestRunner_DECLINED_Path(t *testing.T) {
	const branch = "pilot/GH-7777"
	dir := setupPRGuardRepo(t, branch, false) // no commits — triggers no-commit retry

	backend := &mockSequentialBackend{
		results: []*BackendResult{
			// First call: successful but no commits
			{Success: true, Output: "analysis complete"},
			// Second call (retry): returns DECLINED marker
			{Success: true, Output: "I reviewed the task.", LastAssistantText: "DECLINED: The feature requested already exists in internal/auth/handler.go."},
		},
	}

	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true
	runner.config = &BackendConfig{SkipSelfReview: true}

	task := &Task{
		ID:          "GH-7777",
		Title:       "feat(auth): add JWT handler",
		Description: "Add JWT authentication handler",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    true,
	}

	result, err := runner.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Error("Expected Success=false for declined task")
	}
	if !result.Declined {
		t.Error("Expected result.Declined=true, got false")
	}
	if result.DeclinedReason == "" {
		t.Error("Expected non-empty DeclinedReason")
	}
	if !strings.Contains(result.DeclinedReason, "already exists") {
		t.Errorf("DeclinedReason %q does not contain expected text", result.DeclinedReason)
	}
	// Must NOT be classified as no_changes
	if strings.HasPrefix(result.Error, "no_changes:") {
		t.Errorf("Declined task should not have no_changes error, got: %q", result.Error)
	}
}

// TestRunner_NoChanges_NoDecline verifies that when the retry response contains
// no DECLINED marker, the path falls through to the standard no_changes error. GH-2777.
func TestRunner_NoChanges_NoDecline(t *testing.T) {
	const branch = "pilot/GH-7778"
	dir := setupPRGuardRepo(t, branch, false) // no commits

	backend := &mockSequentialBackend{
		results: []*BackendResult{
			{Success: true, Output: "analysis complete"},
			// Retry produces no commits and no DECLINED marker
			{Success: true, Output: "I reviewed the code but found nothing to change.", LastAssistantText: "The codebase looks good as-is."},
		},
	}

	runner := NewRunnerWithBackend(backend)
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true
	runner.config = &BackendConfig{SkipSelfReview: true}

	task := &Task{
		ID:          "GH-7778",
		Title:       "fix(executor): update handler",
		Description: "Update the handler logic",
		ProjectPath: dir,
		Branch:      branch,
		CreatePR:    true,
	}

	result, err := runner.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Error("Expected failure, got success")
	}
	if result.Declined {
		t.Error("result.Declined should be false when no DECLINED marker present")
	}
	if !strings.HasPrefix(result.Error, "no_changes:") {
		t.Errorf("Expected no_changes error, got: %q", result.Error)
	}
}
