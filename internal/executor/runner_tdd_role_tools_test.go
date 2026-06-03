package executor

import (
	"context"
	"strings"
	"testing"
)

// toolRecordingBackend records the AllowedTools passed to each Execute call
// under its role label, while still running an optional side effect (commit a
// file) so the TDD sequence's CountNewCommits checks observe real changes.
type toolRecordingBackend struct {
	name    string
	role    string
	output  string
	action  func()
	allowed map[string][]string // role -> AllowedTools, shared across the role backends
}

func (b *toolRecordingBackend) Name() string      { return b.name }
func (b *toolRecordingBackend) IsAvailable() bool { return true }

func (b *toolRecordingBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.allowed[b.role] = opts.AllowedTools
	if b.action != nil {
		b.action()
	}
	return &BackendResult{Success: true, Output: b.output}, nil
}

// TestRunTDDSequenceArchitectReadOnlyTools is the F4 integration test: driving
// the full runTDDSequence through recording backends, it asserts the ARCHITECT
// role invocation uses the read-only planning toolset
// (DefaultAllowedToolsPlanning: Read/Grep/Glob) while the TEST-AUTHOR and
// IMPLEMENTER roles use the normal execution toolset so they can write/commit.
//
// Before the fix runTDDRole passed executionToolOptions() to EVERY role, so the
// architect received the writable execution tools (Write/Edit/Bash) and could
// write files; this test fails in that state and passes once the architect is
// scoped to DefaultAllowedToolsPlanning().
func TestRunTDDSequenceArchitectReadOnlyTools(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	// RED gate: tests fail (red satisfied). GREEN gate: pass.
	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})

	// The runner must hand the execution toolset to the writing roles so the
	// architect-vs-execution distinction is observable (not both nil).
	r.config.ClaudeCode = &ClaudeCodeConfig{AllowedTools: DefaultAllowedToolsExecution()}

	allowed := map[string][]string{}
	r.architectBackend = &toolRecordingBackend{
		name: "arch", role: "architect", output: "DESIGN: add func", allowed: allowed,
	}
	r.testAuthorBackend = &toolRecordingBackend{
		name: "ta", role: "test-author", output: "TESTS_ADDED: TestAdd", allowed: allowed,
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	r.implementerBackend = &toolRecordingBackend{
		name: "impl", role: "implementer", output: "IMPLEMENTED", allowed: allowed,
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	if _, err := r.runTDDSequence(s); err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}

	wantPlanning := strings.Join(DefaultAllowedToolsPlanning(), ",")
	wantExecution := strings.Join(DefaultAllowedToolsExecution(), ",")

	if got := strings.Join(allowed["architect"], ","); got != wantPlanning {
		t.Errorf("architect AllowedTools = %q, want read-only planning %q", got, wantPlanning)
	}
	// The architect must NOT receive any write-capable tool.
	for _, tool := range []string{"Write", "Edit", "Bash"} {
		if containsTool(allowed["architect"], tool) {
			t.Errorf("architect AllowedTools contains writable tool %q: %v", tool, allowed["architect"])
		}
	}
	if got := strings.Join(allowed["test-author"], ","); got != wantExecution {
		t.Errorf("test-author AllowedTools = %q, want execution %q", got, wantExecution)
	}
	if got := strings.Join(allowed["implementer"], ","); got != wantExecution {
		t.Errorf("implementer AllowedTools = %q, want execution %q", got, wantExecution)
	}
}

func containsTool(tools []string, want string) bool {
	for _, tool := range tools {
		if tool == want {
			return true
		}
	}
	return false
}
