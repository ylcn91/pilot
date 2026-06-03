package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

type handoffPromptBackend struct {
	name    string
	output  string
	prompts *[]string
	action  func()
}

func (b *handoffPromptBackend) Name() string      { return b.name }
func (b *handoffPromptBackend) IsAvailable() bool { return true }
func (b *handoffPromptBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	*b.prompts = append(*b.prompts, opts.Prompt)
	if b.action != nil {
		b.action()
	}
	return &BackendResult{Success: true, Output: b.output}, nil
}

func TestRunTDDSequenceConsumesTypedArtifactsInRolePrompts(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	prompts := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, &[]string{}, []*QualityOutcome{fail2(), pass()})
	s.planArtifact = pilotapi.NewHandoffArtifact(pilotapi.RolePlan, s.task.ID, "PLAN", "")
	r.architectBackend = &handoffPromptBackend{name: "arch", prompts: prompts, output: "DESIGN"}
	r.testAuthorBackend = &handoffPromptBackend{
		name: "ta", prompts: prompts, output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	r.implementerBackend = &handoffPromptBackend{
		name: "impl", prompts: prompts, output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	if _, err := r.runTDDSequence(s); err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}
	if len(*prompts) != 3 {
		t.Fatalf("captured %d role prompts, want 3", len(*prompts))
	}
	architect := s.tddArtifacts[0]
	testAuthor := s.tddArtifacts[1]

	for _, want := range []string{
		"## Handoff Artifact: architect",
		"TraceHash: " + architect.TraceHash,
		"ParentHash: " + s.planArtifact.TraceHash,
		"DESIGN",
	} {
		if !strings.Contains((*prompts)[1], want) {
			t.Fatalf("test-author prompt missing %q:\n%s", want, (*prompts)[1])
		}
	}
	for _, want := range []string{
		"TraceHash: " + architect.TraceHash,
		"## Handoff Artifact: test-author",
		"TraceHash: " + testAuthor.TraceHash,
		"ParentHash: " + architect.TraceHash,
		"TestAdd",
	} {
		if !strings.Contains((*prompts)[2], want) {
			t.Fatalf("implementer prompt missing %q:\n%s", want, (*prompts)[2])
		}
	}
}
