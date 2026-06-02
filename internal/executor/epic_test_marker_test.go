package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCreateSubIssuesViaGitHub_InjectsAutopilotMetaMarker verifies GH-2695:
// createSubIssuesViaGitHub must inject the <!--autopilot-meta marker into every
// sub-issue body so spec_validator's inherited-spec bailout path can skip the
// full spec check and delegate to the parent's validation result.
func TestCreateSubIssuesViaGitHub_InjectsAutopilotMetaMarker(t *testing.T) {
	bodyFile := filepath.Join(t.TempDir(), "captured_body.txt")

	// Fake "gh" binary that captures the --body argument and returns a valid URL.
	// Parses argv for --body rather than a fixed position so the capture survives
	// arg-order changes (e.g. the GH-3411 --repo flag inserted before --title).
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	scriptContent := fmt.Sprintf("#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = \"--body\" ]; then shift; printf '%%s' \"$1\" > %q; fi\n  shift\ndone\necho https://github.com/owner/repo/issues/77\n", bodyFile)
	if err := os.WriteFile(script, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	origPATH := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+origPATH)

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")
	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "GH-42",
			SourceRepo:    "owner/repo",
			SourceIssueID: "42",
		},
		Subtasks: []PlannedSubtask{
			{Title: "feat(epic): add marker injection", Description: "Inject the autopilot-meta comment.", Order: 1},
		},
	}

	ctx := context.Background()
	created, err := runner.CreateSubIssues(ctx, plan, worktree)
	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(created))
	}

	rawBody, err := os.ReadFile(bodyFile)
	if err != nil {
		t.Fatalf("read captured body: %v", err)
	}
	body := string(rawBody)

	// autopilotMetaRe pattern from internal/adapters/github/spec_validator.go:20
	autopilotMetaRe := regexp.MustCompile(`<!--\s*autopilot-meta\s`)
	if !autopilotMetaRe.MatchString(body) {
		t.Errorf("body does not contain autopilot-meta marker; body = %q", body)
	}

	// parentRefRe pattern: the inherited-spec bailout extracts the parent number from this.
	parentRefRe := regexp.MustCompile(`(?i)Parent:\s*GH-(\d+)`)
	m := parentRefRe.FindStringSubmatch(body)
	if len(m) < 2 {
		t.Errorf("body does not contain a GH-NNN parent reference; body = %q", body)
	} else if m[1] != "42" {
		t.Errorf("parent ref extracted %q, want 42; body = %q", m[1], body)
	}

	// The human-readable "Parent: GH-NNN" prose must also appear below the marker.
	if !strings.Contains(body, "\n\nParent: GH-42\n\n") {
		t.Errorf("human-readable parent prose missing from body; body = %q", body)
	}
}

// TestCreateSubIssues_PinsGhRepoToValidatedOrigin verifies GH-3411: every `gh`
// shell-out (the issue-list dedup check and `gh issue create`) must pass
// --repo <validated origin> so it cannot drift to the fork's upstream parent
// (e.g. ylcn91/pilot) via gh's ambient base-repo resolution. This reproduces
// the #3411 incident shape: a GH-201 parent decomposed into a feat(auth) subtask.
func TestCreateSubIssues_PinsGhRepoToValidatedOrigin(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "argv.log")
	fakeBin := t.TempDir()
	script := filepath.Join(fakeBin, "gh")
	// Record each invocation's full argv (space-joined) one line per call, then emit
	// a URL. `gh issue create` parses the URL for the issue number; the dedup
	// `gh issue list --json` gets the same non-JSON output, so json.Unmarshal fails
	// and the dedup treats it as "no existing children" and proceeds to create.
	scriptContent := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\necho https://github.com/ylcn91/pilot/issues/77\n", argsFile)
	if err := os.WriteFile(script, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(filepath.ListSeparator)+os.Getenv("PATH"))

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetRepoAllowlist(&staticAllowlist{repos: []string{"ylcn91/pilot"}})
	worktree := makeAllowedGitHubWorktree(t, "ylcn91/pilot")
	plan := &EpicPlan{
		ParentTask: &Task{ID: "GH-201", SourceRepo: "ylcn91/pilot", SourceIssueID: "201"},
		Subtasks: []PlannedSubtask{
			{Title: "feat(auth): add OAuth provider integration", Description: "Wire OAuth.", Order: 1},
		},
	}

	created, err := runner.CreateSubIssues(context.Background(), plan, worktree)
	if err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}
	if len(created) != 1 || created[0].Number != 77 {
		t.Fatalf("unexpected created issues: %+v", created)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}

	var sawCreate, sawList bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		switch {
		case strings.HasPrefix(line, "issue create "):
			sawCreate = true
			if !strings.HasPrefix(line, "issue create --repo ylcn91/pilot ") {
				t.Errorf("gh issue create not pinned to validated repo; argv = %q", line)
			}
		case strings.HasPrefix(line, "issue list "):
			sawList = true
			if !strings.Contains(line, "--repo ylcn91/pilot") {
				t.Errorf("gh issue list (dedup) not pinned to validated repo; argv = %q", line)
			}
		}
	}
	if !sawCreate {
		t.Error("expected a `gh issue create` invocation")
	}
	if !sawList {
		t.Error("expected a `gh issue list` (dedup) invocation")
	}
}

// TestCreateSubIssuesViaAdapter_InjectsAutopilotMetaMarker verifies parity with the
// GitHub path: the adapter path must also inject the autopilot-meta marker (GH-2695).
func TestCreateSubIssuesViaAdapter_InjectsAutopilotMetaMarker(t *testing.T) {
	mock := &mockSubIssueCreator{
		Returns: []mockCreateIssueReturn{
			{Identifier: "APP-55", URL: "https://linear.app/team/issue/APP-55"},
		},
	}

	runner := NewRunner()
	runner.SetIssueCreationEnabled(true)
	runner.SetSubIssueCreator(mock)

	plan := &EpicPlan{
		ParentTask: &Task{
			ID:            "APP-10",
			SourceAdapter: "linear",
			SourceIssueID: "APP-10",
			Title:         "Parent epic",
		},
		Subtasks: []PlannedSubtask{
			{Title: "feat(api): add endpoint", Description: "Implement the endpoint.", Order: 1},
		},
	}

	ctx := context.Background()
	if _, err := runner.CreateSubIssues(ctx, plan, ""); err != nil {
		t.Fatalf("CreateSubIssues failed: %v", err)
	}
	if len(mock.Called) != 1 {
		t.Fatalf("expected 1 CreateIssue call, got %d", len(mock.Called))
	}

	body := mock.Called[0].Body
	autopilotMetaRe := regexp.MustCompile(`<!--\s*autopilot-meta\s`)
	if !autopilotMetaRe.MatchString(body) {
		t.Errorf("adapter body does not contain autopilot-meta marker; body = %q", body)
	}

	if !strings.Contains(body, "\n\nParent: APP-10\n\n") {
		t.Errorf("human-readable parent prose missing from adapter body; body = %q", body)
	}
}

func TestFilterPropagatableLabels(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "empty input",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "pilot and no-decompose: keeps no-decompose",
			input: []string{"pilot", "no-decompose"},
			want:  []string{"no-decompose"},
		},
		{
			name:  "all lifecycle labels blocked",
			input: []string{"pilot", "pilot-done", "pilot-failed"},
			want:  []string{},
		},
		{
			name:  "mixed case normalized",
			input: []string{"No-Decompose"},
			want:  []string{"no-decompose"},
		},
		{
			name:  "prefix matches propagate",
			input: []string{"area:executor", "priority:p1", "scope:autopilot", "random-label"},
			want:  []string{"area:executor", "priority:p1", "scope:autopilot"},
		},
		{
			name:  "whitespace trimmed",
			input: []string{"  no-decompose  "},
			want:  []string{"no-decompose"},
		},
		{
			name:  "empty strings skipped",
			input: []string{"", "  ", "no-decompose"},
			want:  []string{"no-decompose"},
		},
		{
			name:  "all lifecycle variants blocked",
			input: []string{"pilot", "pilot-done", "pilot-failed", "pilot-in-progress", "pilot-superseded", "pilot-needs-clarification"},
			want:  []string{},
		},
		{
			name:  "no-plan propagates",
			input: []string{"pilot", "no-plan"},
			want:  []string{"no-plan"},
		},
		{
			name:  "mixed allow and block",
			input: []string{"pilot", "no-decompose", "area:executor", "pilot-done", "priority:p1", "scope:autopilot", "random-label"},
			want:  []string{"no-decompose", "area:executor", "priority:p1", "scope:autopilot"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterPropagatableLabels(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("filterPropagatableLabels(%v) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
